package main

import (
	"fmt"
	"log"
	"os"

	"github.com/kbm-ky/gator/internal/config"
)

func main() {
	//Read config
	configFile, err := config.Read()
	if err != nil {
		log.Fatalf("unable to read config: %v", err)
	}

	// Prepare sub commands
	s := state{config: &configFile}
	cmds := commands{
		handlers: map[string]func(*state, command) error{},
	}
	cmds.register("login", handlerLogin)

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
	config *config.Config
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
	if err := s.config.SetUser(userName); err != nil {
		return fmt.Errorf("unable to set user: %w", err)
	}

	fmt.Printf("user has been set to '%s'\n", userName)

	return nil
}
