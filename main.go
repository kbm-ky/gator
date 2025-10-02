package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
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
