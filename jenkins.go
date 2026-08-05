package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bndr/gojenkins"
)

type JCli struct {
	jenkins *gojenkins.Jenkins
	ctx     context.Context
}

func newJCli() (*JCli, error) {
	if err := ensureConfigured(); err != nil {
		return nil, err
	}

	ctx := context.Background()
	jenkins := gojenkins.CreateJenkins(nil, getAPI(), getUser(), getPassword())
	_, err := jenkins.Init(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to Jenkins %s: %w", getAPI(), err)
	}
	return &JCli{jenkins: jenkins, ctx: ctx}, nil
}

func (c *JCli) hasJob(name string) bool {
	_, err := c.jenkins.GetJob(c.ctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  [debug] hasJob(%s) error: %v, assuming not found\n", name, err)
		return false
	}
	return true
}

func (c *JCli) syncJob(j Job) error {
	xml := generateJobXML(j)
	name := j.Name

	if c.hasJob(name) {
		fmt.Fprintf(os.Stderr, "  %s: updating existing job...\n", name)
		c.jenkins.UpdateJob(c.ctx, name, xml)
		fmt.Fprintf(os.Stderr, "  %s: config updated\n", name)
	} else {
		fmt.Fprintf(os.Stderr, "  %s: creating new job...\n", name)
		_, err := c.jenkins.CreateJob(c.ctx, xml, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: create failed (%v), trying update...\n", name, err)
			c.jenkins.UpdateJob(c.ctx, name, xml)
		}
		fmt.Fprintf(os.Stderr, "  %s: job created/updated\n", name)
	}

	job, err := c.jenkins.GetJob(c.ctx, name)
	if err != nil {
		return fmt.Errorf("get job %s after sync: %w", name, err)
	}

	if j.IsEnabled() {
		if _, err = job.Enable(c.ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: enable skipped (%v), state already set in XML\n", name, err)
		} else {
			fmt.Fprintf(os.Stderr, "  %s: enabled\n", name)
		}
	} else {
		if _, err = job.Disable(c.ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: disable skipped (%v), state already set in XML\n", name, err)
		} else {
			fmt.Fprintf(os.Stderr, "  %s: disabled\n", name)
		}
	}
	return nil
}

func cmdSync() error {
	c, err := newJCli()
	if err != nil {
		return err
	}
	fmt.Printf("Connected to Jenkins %s\n", c.jenkins.Server)

	args := os.Args[2:]
	jobs, err := parseConfigs()
	if err != nil {
		return err
	}

	if len(args) > 0 {
		target := args[0]
		var found *Job
		for i := range jobs {
			if jobs[i].Name == target {
				found = &jobs[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("job '%s' not found in config", target)
		}
		if err := c.syncJob(*found); err != nil {
			return err
		}
		fmt.Printf("sync %s done.\n", target)
		return nil
	}

	succeeded := 0
	failed := 0
	for _, j := range jobs {
		fmt.Printf("sync job: %s\n", j.Name)
		if err := c.syncJob(j); err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED: %v\n", err)
			failed++
			continue
		}
		succeeded++
	}
	fmt.Printf("sync done: %d succeeded, %d failed\n", succeeded, failed)
	return nil
}

func cmdStatus() error {
	args := os.Args[2:]
	if len(args) > 0 {
		c, err := newJCli()
		if err != nil {
			return err
		}
		job, err := c.jenkins.GetJob(c.ctx, args[0])
		if err != nil {
			return fmt.Errorf("get job %s: %w", args[0], err)
		}
		xml, err := job.GetConfig(c.ctx)
		if err != nil {
			return fmt.Errorf("get config for %s: %w", args[0], err)
		}
		fmt.Printf("=== %s config ===\n", args[0])
		fmt.Println(xml)
		return nil
	}

	jobs, err := parseConfigs()
	if err != nil {
		return err
	}
	fmt.Printf("%-30s %-8s %-25s %-12s %s\n", "Job", "Type", "Trigger", "Node", "Enabled")
	fmt.Println(stringsRepeat("-", 90))
	for _, j := range jobs {
		jtype := "cron"
		trigger := j.Config.Cron
		if j.IsGitJob() {
			jtype = "git"
			trigger = "SCM-poll"
		}
		node := j.Config.AssignedNode
		if node == "" {
			node = "any"
		}
		enabled := "Y"
		if !j.IsEnabled() {
			enabled = "N"
		}
		fmt.Printf("%-30s %-8s %-25s %-12s %s\n", j.Name, jtype, trigger, node, enabled)
	}
	return nil
}

func cmdHistory() error {
	c, err := newJCli()
	if err != nil {
		return err
	}
	args := os.Args[2:]

	if len(args) > 0 {
		return c.jobHistory(args[0])
	}
	return c.allHistory()
}

func (c *JCli) jobHistory(name string) error {
	buildIDs, err := c.jenkins.GetAllBuildIds(c.ctx, name)
	if err != nil {
		return fmt.Errorf("get builds for %s: %w", name, err)
	}

	limit := 10
	if len(buildIDs) < limit {
		limit = len(buildIDs)
	}
	fmt.Printf("=== %s last %d builds ===\n", name, limit)
	fmt.Printf("%-6s %-12s %-12s %s\n", "#", "Result", "Duration", "Time")
	fmt.Println(stringsRepeat("-", 60))

	for i, b := range buildIDs {
		if i >= limit {
			break
		}
		build, err := c.jenkins.GetBuild(c.ctx, name, b.Number)
		if err != nil {
			fmt.Printf("%-6d %-12s\n", b.Number, "error")
			continue
		}
		result := build.GetResult()
		if result == "" {
			result = "RUNNING"
		}
		dur := fmt.Sprintf("%.0fs", build.GetDuration()/1000)
		ts := build.GetTimestamp().In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04:05")
		fmt.Printf("%-6d %-12s %-12s %s\n", b.Number, result, dur, ts)
	}
	return nil
}

func (c *JCli) allHistory() error {
	jobs, err := parseConfigs()
	if err != nil {
		return err
	}
	fmt.Printf("%-30s %-10s %-12s %s\n", "Job", "LastBuild", "Result", "Time")
	fmt.Println(stringsRepeat("-", 70))

	for _, j := range jobs {
		job, err := c.jenkins.GetJob(c.ctx, j.Name)
		if err != nil {
			fmt.Printf("%-30s %-10s %s\n", j.Name, "error", err)
			continue
		}
		build, err := job.GetLastBuild(c.ctx)
		if err != nil || build == nil {
			fmt.Printf("%-30s %-10s %-12s %s\n", j.Name, "-", "no builds", "-")
			continue
		}
		result := build.GetResult()
		if result == "" {
			result = "?"
		}
		ts := build.GetTimestamp().In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04:05")
		fmt.Printf("%-30s #%-9d %-12s %s\n", j.Name, build.GetBuildNumber(), result, ts)
	}
	return nil
}

func cmdLog() error {
	if len(os.Args) < 3 {
		fmt.Println("Usage: jenkins-cli log <job_name> [build_number]")
		return nil
	}
	c, err := newJCli()
	if err != nil {
		return err
	}
	jobName := os.Args[2]
	var buildNum int64

	if len(os.Args) >= 4 {
		fmt.Sscanf(os.Args[3], "%d", &buildNum)
	} else {
		job, err := c.jenkins.GetJob(c.ctx, jobName)
		if err != nil {
			return fmt.Errorf("get job %s: %w", jobName, err)
		}
		build, err := job.GetLastBuild(c.ctx)
		if err != nil || build == nil {
			return fmt.Errorf("%s has no builds", jobName)
		}
		buildNum = build.GetBuildNumber()
	}

	fmt.Printf("=== %s #%d console ===\n", jobName, buildNum)
	build, err := c.jenkins.GetBuild(c.ctx, jobName, buildNum)
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}
	fmt.Println(build.GetConsoleOutput(c.ctx))
	return nil
}

func cmdBuild() error {
	if len(os.Args) < 3 {
		fmt.Println("Usage: jenkins-cli build <job_name>")
		return nil
	}
	c, err := newJCli()
	if err != nil {
		return err
	}
	jobName := os.Args[2]
	if _, err := c.jenkins.BuildJob(c.ctx, jobName, nil); err != nil {
		return fmt.Errorf("build %s: %w", jobName, err)
	}
	fmt.Printf("triggered: %s\n", jobName)
	return nil
}

func cmdList() error {
	c, err := newJCli()
	if err != nil {
		return err
	}
	jobs, err := c.jenkins.GetAllJobNames(c.ctx)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	fmt.Printf("%-40s %-8s %-12s %s\n", "Job", "Type", "Status", "Enabled")
	fmt.Println(stringsRepeat("-", 70))
	for _, j := range jobs {
		jtype := "job"
		enabled := "Y"
		if stringsContains(j.Color, "disabled") {
			enabled = "N"
		}
		fmt.Printf("%-40s %-8s %-12s %s\n", j.Name, jtype, j.Color, enabled)
	}
	return nil
}

func cmdEnable() error {
	if len(os.Args) < 3 {
		fmt.Println("Usage: jenkins-cli enable <job_name>")
		return nil
	}
	c, err := newJCli()
	if err != nil {
		return err
	}
	name := os.Args[2]
	job, err := c.jenkins.GetJob(c.ctx, name)
	if err != nil {
		return fmt.Errorf("get job %s: %w", name, err)
	}
	if _, err := job.Enable(c.ctx); err != nil {
		return err
	}
	fmt.Printf("enabled: %s\n", name)
	return nil
}

func cmdDisable() error {
	if len(os.Args) < 3 {
		fmt.Println("Usage: jenkins-cli disable <job_name>")
		return nil
	}
	c, err := newJCli()
	if err != nil {
		return err
	}
	name := os.Args[2]
	job, err := c.jenkins.GetJob(c.ctx, name)
	if err != nil {
		return fmt.Errorf("get job %s: %w", name, err)
	}
	if _, err := job.Disable(c.ctx); err != nil {
		return err
	}
	fmt.Printf("disabled: %s\n", name)
	return nil
}

func cmdDelete() error {
	if len(os.Args) < 3 {
		fmt.Println("Usage: jenkins-cli delete <job_name>")
		return nil
	}
	c, err := newJCli()
	if err != nil {
		return err
	}
	name := os.Args[2]
	if _, err := c.jenkins.DeleteJob(c.ctx, name); err != nil {
		return err
	}
	fmt.Printf("deleted: %s\n", name)
	return nil
}

func cmdDiff() error {
	c, err := newJCli()
	if err != nil {
		return err
	}
	fmt.Printf("Connected to Jenkins %s\n", c.jenkins.Server)

	args := os.Args[2:]
	jobs, err := parseConfigs()
	if err != nil {
		return err
	}

	if len(args) > 0 {
		target := args[0]
		var found *Job
		for i := range jobs {
			if jobs[i].Name == target {
				found = &jobs[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("job '%s' not found in config", target)
		}
		return c.diffJob(*found)
	}

	changed := 0
	upToDate := 0
	newJobs := 0
	for _, j := range jobs {
		localXML := generateJobXML(j)
		remoteXML, err := c.getRemoteXML(j.Name)
		if err != nil {
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
				fmt.Printf("+ %s (new - not on Jenkins)\n", j.Name)
				newJobs++
				continue
			}
			fmt.Printf("? %s (error: %v)\n", j.Name, err)
			continue
		}
		if localXML == remoteXML {
			upToDate++
			continue
		}
		changed++
		fmt.Printf("\n=== %s ===\n%s", j.Name, unifiedDiff(remoteXML, localXML, j.Name))
	}

	fmt.Printf("\nSummary: %d up to date, %d changed, %d new\n", upToDate, changed, newJobs)
	return nil
}

func (c *JCli) diffJob(j Job) error {
	localXML := generateJobXML(j)
	remoteXML, err := c.getRemoteXML(j.Name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			fmt.Printf("+ %s (new - not on Jenkins)\n", j.Name)
			return nil
		}
		return fmt.Errorf("fetch remote config for %s: %w", j.Name, err)
	}
	if localXML == remoteXML {
		fmt.Printf("%s: up to date\n", j.Name)
		return nil
	}
	fmt.Println(unifiedDiff(remoteXML, localXML, j.Name))
	return nil
}

func (c *JCli) getRemoteXML(name string) (string, error) {
	job, err := c.jenkins.GetJob(c.ctx, name)
	if err != nil {
		return "", err
	}
	return job.GetConfig(c.ctx)
}

func unifiedDiff(old, new, name string) string {
	oldFile, err := writeTemp(old, "jenkins-cli-old-")
	if err != nil {
		return fmt.Sprintf("(diff error: %v)\n", err)
	}
	defer os.Remove(oldFile)

	newFile, err := writeTemp(new, "jenkins-cli-new-")
	if err != nil {
		return fmt.Sprintf("(diff error: %v)\n", err)
	}
	defer os.Remove(newFile)

	cmd := exec.Command("diff", "-u", "--label", "jenkins/"+name, oldFile, "--label", "local/"+name, newFile)
	out, _ := cmd.Output()
	// diff exits with code 1 when files differ
	if len(out) == 0 {
		return ""
	}
	return string(out)
}

func writeTemp(content, prefix string) (string, error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}
