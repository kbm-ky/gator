package main

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/kbm-ky/gator/internal/config"
	"github.com/kbm-ky/gator/internal/database"
	_ "github.com/lib/pq"
)

func main() {
	//Read config
	configFile, err := config.Read()
	if err != nil {
		log.Fatalf("unable to read config: %v", err)
	}

	//Prepare database
	db, err := sql.Open("postgres", configFile.DbUrl)
	if err != nil {
		log.Fatalf("unable to open databse: %s", configFile.DbUrl)
	}

	dbQueries := database.New(db)

	// Prepare sub commands
	s := state{db: dbQueries, cfg: &configFile}
	cmds := commands{
		handlers: map[string]func(*state, command) error{},
	}
	cmds.register("login", handlerLogin)
	cmds.register("register", handlerRegister)
	cmds.register("reset", handlerReset)
	cmds.register("users", handlerUsers)
	cmds.register("agg", handlerAgg)
	cmds.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	cmds.register("feeds", handlerFeeds)
	cmds.register("follow", middlewareLoggedIn(handlerFollow))
	cmds.register("following", middlewareLoggedIn(handlerFollowing))
	cmds.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	cmds.register("browse", middlewareLoggedIn(handlerBrowse))

	//finally check command line and dispatch
	if len(os.Args) < 2 {
		fmt.Printf("expecting sub-command, got nothing\n")
		os.Exit(1)
	}

	command := command{
		name: os.Args[1],
		args: os.Args[2:],
	}

	if err := cmds.run(&s, command); err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}
}

type state struct {
	db  *database.Queries
	cfg *config.Config
}

type command struct {
	name string
	args []string
}

type commands struct {
	handlers map[string]func(*state, command) error
}

func (c *commands) run(s *state, cmd command) error {
	handler, ok := c.handlers[cmd.name]
	if !ok {
		return fmt.Errorf("command not found: %s", cmd.name)
	}

	return handler(s, cmd)
}

func (c *commands) register(name string, f func(*state, command) error) {
	c.handlers[name] = f
}

func handlerLogin(s *state, cmd command) error {
	//Check args
	if len(cmd.args) != 1 {
		return fmt.Errorf("login command expects 1 argument")
	}

	userName := cmd.args[0]
	_, err := s.db.GetUser(context.Background(), userName)
	if err != nil {
		log.Printf("user does not exist! %s\n", userName)
		os.Exit(1)
	}

	if err := s.cfg.SetUser(userName); err != nil {
		return fmt.Errorf("unable to set user: %w", err)
	}

	fmt.Printf("user has been set to '%s'\n", userName)

	return nil
}

func handlerRegister(s *state, cmd command) error {
	//Check args
	if len(cmd.args) != 1 {
		return fmt.Errorf("register command expects 1 argument: name")
	}
	name := cmd.args[0]

	now := time.Now()
	userParams := database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      name,
	}
	user, err := s.db.CreateUser(context.Background(), userParams)
	if err != nil {
		log.Printf("name already exists! %s", name)
		os.Exit(1)
	}

	s.cfg.SetUser(name)
	fmt.Printf("user '%s' created\n", name)
	fmt.Printf("user = %v\n", user)

	return nil
}

func handlerReset(s *state, cmd command) error {
	err := s.db.DeleteAllUsers(context.Background())
	if err != nil {
		log.Printf("unable to delete all users! %v", err)
		os.Exit(1)
	}
	return nil
}

func handlerUsers(s *state, cmd command) error {
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		log.Printf("unable to get all usrs! %v", err)
		os.Exit(1)
	}

	for _, user := range users {
		if user.Name == s.cfg.CurrentUserName {
			fmt.Printf("* %s (current)\n", user.Name)
		} else {
			fmt.Printf("* %s\n", user.Name)
		}
	}

	return nil
}

func handlerAgg(s *state, cmd command) error {
	//check args
	if len(cmd.args) != 1 {
		return fmt.Errorf("agg expects 1 argument, time_between_reqs")
	}
	time_between_reqs := cmd.args[0]

	duration, err := time.ParseDuration(time_between_reqs)
	if err != nil {
		return fmt.Errorf("unable to parse time between requests: %w", err)
	}

	ticker := time.NewTicker(duration)
	for ; ; <-ticker.C {
		fmt.Printf("Collect feeds every %s\n", duration.String())
		if err := scrapeFeeds(s); err != nil {
			return fmt.Errorf("unable to scrap feed: %w", err)
		}
		fmt.Println()
	}
	// rssFeed, err := fetchFeed(context.Background(), "https://www.wagslane.dev/index.xml")
	// if err != nil {
	// 	return fmt.Errorf("unable to fetch feed: %w", err)
	// }

	// fmt.Printf("%v\n", *rssFeed)

	return nil
}

func handlerAddFeed(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 2 {
		return fmt.Errorf("addfeed expects two args: name and url")
	}

	name, url := cmd.args[0], cmd.args[1]

	now := time.Now()
	params := database.CreateFeedParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      name,
		Url:       url,
		UserID:    user.ID,
	}
	feed, err := s.db.CreateFeed(context.Background(), params)
	if err != nil {
		return fmt.Errorf("unable to create feed! %w", err)
	}

	fmt.Printf("feed created:\n")
	fmt.Printf("%v\n", feed)

	now = time.Now()
	feedFollowArgs := database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    user.ID,
		FeedID:    feed.ID,
	}

	_, err = s.db.CreateFeedFollow(context.Background(), feedFollowArgs)
	if err != nil {
		return fmt.Errorf("unable to create feed_follow: %w", err)
	}

	return nil
}

func handlerFeeds(s *state, cmd command) error {
	//No args
	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return fmt.Errorf("unable to get feeds! %w", err)
	}

	//iterate and print
	for _, feed := range feeds {
		user, err := s.db.GetUserById(context.Background(), feed.UserID)
		if err != nil {
			return fmt.Errorf("unable to get user by id! %w", err)
		}
		fmt.Printf("Name: %s\n", feed.Name)
		fmt.Printf("URL: %s\n", feed.Url)
		fmt.Printf("User: %s\n", user.Name)
		fmt.Println()
	}

	return nil
}

func handlerFollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("follow expects 1 argument: url")
	}
	url := cmd.args[0]
	feed, err := s.db.GetFeedByUrl(context.Background(), url)
	if err != nil {
		return fmt.Errorf("unable to get feed by url: %w", err)
	}

	now := time.Now()
	params := database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    user.ID,
		FeedID:    feed.ID,
	}

	feed_follow, err := s.db.CreateFeedFollow(context.Background(), params)
	if err != nil {
		return fmt.Errorf("unable to create feed_follow: %w", err)
	}

	fmt.Printf("created feed_follow:\n")
	fmt.Printf("Feed Name: %s\n", feed_follow.FeedName)
	fmt.Printf("User Name: %s\n", feed_follow.UserName)

	return nil
}

func handlerFollowing(s *state, cmd command, user database.User) error {
	currentUser := user.Name
	feed_follows, err := s.db.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return fmt.Errorf("unable to get feed follows for user: %w", err)
	}

	fmt.Printf("Feeds %s is following:\n", currentUser)
	for _, feed_follow := range feed_follows {
		fmt.Println(feed_follow.FeedName)
	}

	return nil
}

func handlerUnfollow(s *state, cmd command, user database.User) error {
	//check args
	if len(cmd.args) != 1 {
		return fmt.Errorf("unfollow expects 1 argument, feed_url")
	}
	url := cmd.args[0]

	feed, err := s.db.GetFeedByUrl(context.Background(), url)
	if err != nil {
		return fmt.Errorf("unable to get feed by url: %w", err)
	}

	args := database.DeleteFeedFollowParams{
		UserID: user.ID,
		FeedID: feed.ID,
	}

	err = s.db.DeleteFeedFollow(context.Background(), args)
	if err != nil {
		return fmt.Errorf("unable to delete feed follow: %w", err)
	}

	fmt.Printf("User %s unfollowed feed:\n", user.Name)
	fmt.Printf("%s\n", feed.Url)

	return nil
}

func handlerBrowse(s *state, cmd command, user database.User) error {
	limitStr := "2"
	if len(cmd.args) > 0 {
		limitStr = cmd.args[0]
	}

	limit, err := strconv.ParseInt(limitStr, 10, 32)
	if err != nil {
		return fmt.Errorf("unable to parse limit: %s [%w]", limitStr, err)
	}
	if limit < 2 {
		log.Printf("changing limit to 2")
		limit = 2
	}

	args := database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  int32(limit),
	}

	posts, err := s.db.GetPostsForUser(context.Background(), args)
	if err != nil {
		return fmt.Errorf("unable to get posts for user: %w", err)
	}

	for _, post := range posts {
		fmt.Printf("Title: %s\n", post.Title.String)
		fmt.Printf("Url: %s\n", post.Url)
		fmt.Printf("Description: %s\n", post.Description.String)
		fmt.Println()
	}

	return nil
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
		if err != nil {
			return fmt.Errorf("unable to get user: %w", err)
		}

		return handler(s, cmd, user)
	}
}

func scrapeFeeds(s *state) error {
	feed, err := s.db.GetNextFeedToFetch(context.Background())
	if err != nil {
		return fmt.Errorf("unable to get next feed to fetch: %w", err)
	}

	now := time.Now()
	markArgs := database.MarkFeedFetchedParams{
		ID:            feed.ID,
		LastFetchedAt: sql.NullTime{Time: now, Valid: true},
		UpdatedAt:     now,
	}
	err = s.db.MarkFeedFetched(context.Background(), markArgs)
	if err != nil {
		return fmt.Errorf("unable to mark feed fetched: %w", err)
	}

	rssFeed, err := fetchFeed(context.Background(), feed.Url)
	if err != nil {
		log.Printf("unable to fetch feed: %v", err)
	}

	// fmt.Printf("Channel: %s\n", rssFeed.Channel.Title)
	// for _, item := range rssFeed.Channel.Item {
	// 	fmt.Printf("Title: %s\n", item.Title)
	// }

	//Saving to posts
	for _, item := range rssFeed.Channel.Item {
		now := time.Now()

		// t, err := time.Parse(time.RFC3339, item.PubDate)
		t, err := parseTime(item.PubDate)
		publishedAt := sql.NullTime{}
		if err != nil {
			log.Printf("unable to parse time: %s", item.PubDate)
		} else {
			publishedAt.Time = t
			publishedAt.Valid = true
		}

		title := sql.NullString{}
		if item.Title != "" {
			title.String = item.Title
			title.Valid = true
		}

		descr := sql.NullString{}
		if item.Description != "" {
			descr.String = item.Description
			descr.Valid = true
		}

		args := database.CreatePostParams{
			ID:          uuid.New(),
			CreatedAt:   now,
			UpdatedAt:   now,
			Title:       title,
			Url:         item.Link,
			Description: descr,
			PublishedAt: publishedAt,
			FeedID:      feed.ID,
		}
		_, err = s.db.CreatePost(context.Background(), args)
		if err != nil {
			log.Printf("unable to create post: %v", err)
		}

	}

	return nil
}

func parseTime(timeString string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, timeString)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(time.RFC1123Z, timeString)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeString)
}

type RSSFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Item        []RSSItem `xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to make new requst: %w", err)
	}
	req.Header.Set("User-Agent", "gator")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to do request: %w", err)
	}
	defer resp.Body.Close()

	xmlBlob, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	var rssFeed RSSFeed
	if err := xml.Unmarshal(xmlBlob, &rssFeed); err != nil {
		return nil, fmt.Errorf("unable to unmarshal xml: %w", err)
	}

	rssFeed.Channel.Title = html.UnescapeString(rssFeed.Channel.Title)
	rssFeed.Channel.Description = html.UnescapeString(rssFeed.Channel.Description)
	for i := range rssFeed.Channel.Item {
		raw := rssFeed.Channel.Item[i].Description
		rssFeed.Channel.Item[i].Description = html.UnescapeString(raw)
		raw = rssFeed.Channel.Item[i].Title
		rssFeed.Channel.Item[i].Title = html.UnescapeString(raw)
	}

	return &rssFeed, nil
}
