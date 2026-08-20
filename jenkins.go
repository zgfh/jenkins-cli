package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// JenkinsClient uses direct HTTP API calls instead of gojenkins SDK.
// gojenkins SDK has issues parsing HTML responses that Jenkins returns
// for some endpoints (createItem, enable, disable, doDelete).
type JenkinsClient struct {
	baseURL    string
	user       string
	password   string
	crumbField string
	crumbValue string
	client     *http.Client
}

func newJenkinsClient(apiURL, user, password string) (*JenkinsClient, error) {
	jar, _ := cookiejar.New(nil)
	jc := &JenkinsClient{
		baseURL:  strings.TrimRight(apiURL, "/"),
		user:     user,
		password: password,
		client: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
	}
	if err := jc.init(); err != nil {
		return nil, fmt.Errorf("connect to Jenkins %s: %w", apiURL, err)
	}
	return jc, nil
}

func (c *JenkinsClient) init() error {
	// Fetch CSRF crumb — this also authenticates and stores the session cookie
	if err := c.fetchCrumb2(); err != nil {
		return fmt.Errorf("connect to Jenkins %s: %w", c.baseURL, err)
	}
	return nil
}

func (c *JenkinsClient) fetchCrumb() {}

func (c *JenkinsClient) fetchCrumb2() error {
	resp, err := c.do("GET", "/crumbIssuer/api/xml?xpath=concat(//crumbRequestField,%22:%22,//crumb)", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	parts := strings.SplitN(string(data), ":", 2)
	if len(parts) == 2 {
		c.crumbField = parts[0]
		c.crumbValue = parts[1]
	}
	return nil
}

func (c *JenkinsClient) do(method, path string, body io.Reader) (*http.Response, error) {
	return c.doWithTimeout(method, path, body, 0)
}

// doWithTimeout 与 do 相同，但可指定超时（0 表示用默认 client 超时 30s）。
// 用于删除等耗时操作：Jenkins 同步清理大量构建历史会超过默认 30s，导致误报超时。
func (c *JenkinsClient) doWithTimeout(method, path string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.password)
	if c.crumbField != "" && c.crumbValue != "" && method != "GET" {
		req.Header.Set(c.crumbField, c.crumbValue)
	}
	if method == "POST" && body != nil {
		req.Header.Set("Content-Type", "application/xml")
	}
	client := c.client
	if timeout > 0 {
		client = &http.Client{Jar: c.client.Jar, Timeout: timeout}
	}
	return client.Do(req)
}

func (c *JenkinsClient) doPost(path string, body io.Reader) (int, error) {
	resp, err := c.do("POST", path, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (c *JenkinsClient) doGet(path string) ([]byte, error) {
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// hasJob checks if a job exists on the Jenkins server.
func (c *JenkinsClient) hasJob(name string) bool {
	resp, err := c.do("GET", "/job/"+url.PathEscape(name)+"/api/json", nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

// Server returns the base URL.
func (c *JenkinsClient) Server() string {
	return c.baseURL
}

// --- Job CRUD ---

func (c *JenkinsClient) createJob(name, xml string) error {
	resp, err := c.do("POST", "/createItem?name="+url.QueryEscape(name), strings.NewReader(xml))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Jenkins returns 200 with HTML on success, 400/500 with error page
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 500)]))
	}
	return nil
}

func (c *JenkinsClient) updateJob(name, xml string) error {
	code, err := c.doPost("/job/"+url.PathEscape(name)+"/config.xml", strings.NewReader(xml))
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

func (c *JenkinsClient) getJobConfig(name string) (string, error) {
	data, err := c.doGet("/job/" + url.PathEscape(name) + "/config.xml")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *JenkinsClient) deleteJob(name string) error {
	// 删除大量构建历史的 job 很慢（Jenkins 同步清理构建记录），用长超时避免误报超时
	resp, err := c.doWithTimeout("POST", "/job/"+url.PathEscape(name)+"/doDelete", nil, 10*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 302 redirect 由 http.Client 自动跟随，最终 200；404 表示 job 不存在
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 300)]))
	}
	return nil
}

func (c *JenkinsClient) enableJob(name string) error {
	code, err := c.doPost("/job/"+url.PathEscape(name)+"/enable", nil)
	if err != nil {
		return err
	}
	if code >= 400 && code != 302 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

func (c *JenkinsClient) disableJob(name string) error {
	code, err := c.doPost("/job/"+url.PathEscape(name)+"/disable", nil)
	if err != nil {
		return err
	}
	if code >= 400 && code != 302 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

func (c *JenkinsClient) buildJob(name string) error {
	code, err := c.doPost("/job/"+url.PathEscape(name)+"/build", nil)
	if err != nil {
		return err
	}
	// 201 Created is expected
	if code != 201 && code >= 400 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

// --- Data types ---

type jenkinsInnerJob struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type jenkinsJobList struct {
	Jobs []jenkinsInnerJob `json:"jobs"`
}

type jenkinsJobDetail struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type jenkinsBuildID struct {
	Number int64 `json:"number"`
}

type jenkinsBuildIDs struct {
	Builds []jenkinsBuildID `json:"builds"`
}

type jenkinsBuild struct {
	Number    int64  `json:"number"`
	Result    string `json:"result"`
	Duration  int64  `json:"duration"`
	Timestamp int64  `json:"timestamp"`
}

// --- Query methods ---

func (c *JenkinsClient) getAllJobNames() ([]jenkinsInnerJob, error) {
	data, err := c.doGet("/api/json?tree=jobs[name,color]")
	if err != nil {
		return nil, err
	}
	var list jenkinsJobList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list.Jobs, nil
}

func (c *JenkinsClient) getAllBuildIDs(name string) ([]jenkinsBuildID, error) {
	data, err := c.doGet("/job/" + url.PathEscape(name) + "/api/json?tree=builds[number]")
	if err != nil {
		return nil, err
	}
	var ids jenkinsBuildIDs
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, err
	}
	return ids.Builds, nil
}

func (c *JenkinsClient) getBuild(name string, number int64) (*jenkinsBuild, error) {
	path := fmt.Sprintf("/job/%s/%d/api/json", url.PathEscape(name), number)
	data, err := c.doGet(path)
	if err != nil {
		return nil, err
	}
	var b jenkinsBuild
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *JenkinsClient) getLastBuild(name string) (*jenkinsBuild, error) {
	data, err := c.doGet("/job/" + url.PathEscape(name) + "/lastBuild/api/json")
	if err != nil {
		return nil, err
	}
	var b jenkinsBuild
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *JenkinsClient) getConsoleOutput(name string, number int64) (string, error) {
	path := fmt.Sprintf("/job/%s/%d/consoleText", url.PathEscape(name), number)
	data, err := c.doGet(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *JenkinsClient) getJob(name string) (*jenkinsJobDetail, error) {
	data, err := c.doGet("/job/" + url.PathEscape(name) + "/api/json?tree=name,color")
	if err != nil {
		return nil, err
	}
	var j jenkinsJobDetail
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// ========== CLI Command Implementations ==========

type JCli struct {
	jenkins *JenkinsClient
}

func newJCli() (*JCli, error) {
	if err := ensureConfigured(); err != nil {
		return nil, err
	}

	jenkins, err := newJenkinsClient(getAPI(), getUser(), getPassword())
	if err != nil {
		return nil, err
	}
	return &JCli{jenkins: jenkins}, nil
}

func (c *JCli) syncJob(j Job) error {
	xml := generateJobXML(j)
	name := j.Name

	exists := c.jenkins.hasJob(name)
	if !exists {
		fmt.Fprintf(os.Stderr, "  %s: creating new job (len=%d)...\n", name, len(xml))
		if err := c.jenkins.createJob(name, xml); err != nil {
			return fmt.Errorf("create job: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s: created\n", name)
	} else {
		fmt.Fprintf(os.Stderr, "  %s: updating config...\n", name)
	}

	// Always reconfig (same as Python approach)
	if err := c.jenkins.updateJob(name, xml); err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  %s: config updated\n", name)

	// Enable/Disable
	if j.IsEnabled() {
		if err := c.jenkins.enableJob(name); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: enable failed (job will use XML state): %v\n", name, err)
		} else {
			fmt.Fprintf(os.Stderr, "  %s: enabled\n", name)
		}
	} else {
		if err := c.jenkins.disableJob(name); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: disable failed (job will use XML state): %v\n", name, err)
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
	fmt.Printf("Connected to Jenkins %s\n", c.jenkins.Server())

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
		xml, err := c.jenkins.getJobConfig(args[0])
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
	buildIDs, err := c.jenkins.getAllBuildIDs(name)
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
		build, err := c.jenkins.getBuild(name, b.Number)
		if err != nil {
			fmt.Printf("%-6d %-12s\n", b.Number, "error")
			continue
		}
		result := build.Result
		if result == "" {
			result = "RUNNING"
		}
		dur := fmt.Sprintf("%.0fs", float64(build.Duration)/1000)
		ts := time.UnixMilli(build.Timestamp).In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04:05")
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
		build, err := c.jenkins.getLastBuild(j.Name)
		if err != nil || build == nil {
			fmt.Printf("%-30s %-10s %-12s %s\n", j.Name, "-", "no builds", "-")
			continue
		}
		result := build.Result
		if result == "" {
			result = "?"
		}
		ts := time.UnixMilli(build.Timestamp).In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04:05")
		fmt.Printf("%-30s #%-9d %-12s %s\n", j.Name, build.Number, result, ts)
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
		build, err := c.jenkins.getLastBuild(jobName)
		if err != nil || build == nil {
			return fmt.Errorf("%s has no builds", jobName)
		}
		buildNum = build.Number
	}

	fmt.Printf("=== %s #%d console ===\n", jobName, buildNum)
	output, err := c.jenkins.getConsoleOutput(jobName, buildNum)
	if err != nil {
		return fmt.Errorf("get build: %w", err)
	}
	fmt.Println(output)
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
	if err := c.jenkins.buildJob(jobName); err != nil {
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
	jobs, err := c.jenkins.getAllJobNames()
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
	if err := c.jenkins.enableJob(name); err != nil {
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
	if err := c.jenkins.disableJob(name); err != nil {
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
	if err := c.jenkins.deleteJob(name); err != nil {
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
	fmt.Printf("Connected to Jenkins %s\n", c.jenkins.Server())

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
	xml, err := c.jenkins.getJobConfig(name)
	if err != nil {
		return "", err
	}
	// Jenkins returns HTML error page (not 404) for non-existent jobs
	if strings.HasPrefix(strings.TrimSpace(xml), "<") && !strings.HasPrefix(strings.TrimSpace(xml), "<?xml") {
		return "", fmt.Errorf("404 job %s not found", name)
	}
	return xml, nil
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
