package main

import (
	"fmt"
	"os"
)

var (
	Version = "dev"
)

func main() {
	loadConfig()

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	cmd := os.Args[1]
	commands := map[string]func() error{
		"init":    cmdInit,
		"sync":    cmdSync,
		"status":  cmdStatus,
		"history": cmdHistory,
		"log":     cmdLog,
		"build":   cmdBuild,
		"list":    cmdList,
		"enable":  cmdEnable,
		"disable": cmdDisable,
		"delete":  cmdDelete,
		"version": cmdVersion,
	}

	fn, ok := commands[cmd]
	if !ok {
		fmt.Printf("Unknown command: %s\n\n", cmd)
		printHelp()
		os.Exit(1)
	}

	if err := fn(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdVersion() error {
	fmt.Printf("jenkins-cli %s\n", Version)
	return nil
}

func printHelp() {
	fmt.Print(`Jenkins CLI - Manage Jenkins jobs from the command line

Usage: jenkins-cli <command> [args...]

Commands:
  init                Generate ~/.jenkinscli/config interactively
  version             Show version
  sync                Sync all jobs from YAML config to Jenkins
  sync <job>          Sync specified job
  status              Show all job config summary (from local YAML)
  status <job>        Show job config XML (from Jenkins)
  history             Show all jobs last build status
  history <job>       Show job build history (last 10)
  log <job>           Show last build console log
  log <job> <n>       Show build #n console log
  build <job>         Trigger a build
  list                List all jobs on Jenkins server
  enable <job>        Enable a job
  disable <job>       Disable a job
  delete <job>        Delete a job

Config file: ~/.jenkinscli/config (key=value format)
Env vars (fallback if not in config): JENKINS_API, JENKINS_USER, JENKINS_PASSWORD, CONFIG_DIR
`)
}

func stringsRepeat(s string, n int) string {
	r := ""
	for i := 0; i < n; i++ {
		r += s
	}
	return r
}

func stringsContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if i+len(sub) > len(s) {
			break
		}
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
