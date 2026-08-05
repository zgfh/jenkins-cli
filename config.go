package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type JobConfig struct {
	GitURL       string `yaml:"git-url"`
	GitAuthID    string `yaml:"git-auth-id"`
	GitBranch    string `yaml:"git-branch"`
	AssignedNode string `yaml:"assignedNode"`
	Cron         string `yaml:"cron"`
	BuildScript  string `yaml:"build-script"`
}

type Job struct {
	Name   string    `yaml:"name"`
	Enable *bool     `yaml:"enable"`
	Config JobConfig `yaml:"config"`
}

type JobsFile struct {
	Jobs []Job `yaml:"jobs"`
}

func (j Job) IsEnabled() bool {
	if j.Enable == nil {
		return true
	}
	return *j.Enable
}

func (j Job) IsGitJob() bool {
	return j.Config.GitURL != ""
}

func parseConfigs() ([]Job, error) {
	pattern := getConfigDir()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var allJobs []Job
	for _, f := range matches {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var jf JobsFile
		if err := yaml.Unmarshal(data, &jf); err != nil {
			return nil, err
		}
		allJobs = append(allJobs, jf.Jobs...)
	}
	return allJobs, nil
}

func findJob(name string) (*Job, error) {
	jobs, err := parseConfigs()
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.Name == name {
			return &j, nil
		}
	}
	return nil, nil
}

// --- config file: ~/.jenkinscli/config ---

var configMap map[string]string

func loadConfig() {
	configMap = make(map[string]string)
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	f, err := os.Open(filepath.Join(home, ".jenkinscli", "config"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			configMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
}

func getConfig(key, defaultVal string) string {
	if v, ok := configMap[key]; ok && v != "" {
		return v
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getPassword() string {
	pass := getConfig("JENKINS_PASSWORD", "")
	if pass == "" {
		pass = "dangerous"
	}
	return pass
}

func getAPI() string {
	api := getConfig("JENKINS_API", "")
	if api == "" {
		api = "http://127.0.0.1:8080"
	}
	return strings.TrimRight(api, "/")
}

func getUser() string {
	return getConfig("JENKINS_USER", "root")
}

func getConfigDir() string {
	return getConfig("CONFIG_DIR", "config/*.yaml")
}

func ensureConfigured() error {
	if _, ok := configMap["JENKINS_API"]; !ok && os.Getenv("JENKINS_API") == "" {
		return fmt.Errorf("Jenkins not configured.\n\nSet up with:\n  mkdir -p ~/.jenkinscli\n  cat > ~/.jenkinscli/config << 'EOF'\n  JENKINS_API=https://jenkins.example.com\n  JENKINS_USER=admin\n  JENKINS_PASSWORD=your-token\n  CONFIG_DIR=/path/to/config/*.yaml\n  EOF\n\nOr export JENKINS_API, JENKINS_USER, JENKINS_PASSWORD as env vars.")
	}
	return nil
}

func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jenkinscli", "config"), nil
}

func cmdInit() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	api := prompt("Jenkins URL", getConfig("JENKINS_API", "https://jenkins.example.com"))
	user := prompt("Username", getConfig("JENKINS_USER", "admin"))
	pass := prompt("Password/Token", getConfig("JENKINS_PASSWORD", ""))
	cfgDir := prompt("Config dir (YAML files)", getConfig("CONFIG_DIR", "config/*.yaml"))

	content := fmt.Sprintf("# Jenkins CLI config\nJENKINS_API=%s\nJENKINS_USER=%s\nJENKINS_PASSWORD=%s\nCONFIG_DIR=%s\n", api, user, pass, cfgDir)

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Config written to %s\n", path)
	return nil
}

func prompt(label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}
