//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"
)

func main() {
	os.Args = append(os.Args, "/Users/zzg/code/gitops/jenkins/config/aihome.yaml") // dummy for getConfigDir
	if len(os.Args) < 2 {
		os.Exit(1)
	}
	name := os.Args[1]
	_ = os.Setenv("CONFIG_DIR", "/Users/zzg/code/gitops/jenkins/config/aihome.yaml")
	loadConfig()
	jobs, err := parseConfigs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}
	for _, j := range jobs {
		if j.Name == name {
			xml := generateJobXML(j)
			fmt.Print(xml)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "job %s not found\n", name)
	os.Exit(1)
}
