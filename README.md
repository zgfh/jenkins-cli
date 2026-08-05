# jenkins-cli

A single-binary Jenkins job management CLI, rewritten from Python to Go.

## Install

```bash
# Download pre-built binary from releases:
# https://github.com/zgfh/jenkins-cli/releases

# Or source install:
go install github.com/zgfh/jenkins-cli@latest
```

## Quick Start

```bash
# Interactive config setup
jenkins-cli init

# OR create config manually
mkdir -p ~/.jenkinscli
cat > ~/.jenkinscli/config << 'EOF'
JENKINS_API=https://jenkins.example.com
JENKINS_USER=admin
JENKINS_PASSWORD=your-token
CONFIG_DIR=/path/to/jenkins/config/*.yaml
EOF
chmod 600 ~/.jenkinscli/config
```

## Usage

```bash
jenkins-cli status            # Show all jobs from local config
jenkins-cli list              # List all jobs on Jenkins server
jenkins-cli history           # Last build status for all jobs
jenkins-cli history <job>     # Last 10 builds of a job
jenkins-cli log <job>         # Console log of last build
jenkins-cli log <job> <n>     # Console log of build #n
jenkins-cli build <job>       # Trigger a build
jenkins-cli sync              # Sync all YAML configs to Jenkins
jenkins-cli sync <job>        # Sync a single job
jenkins-cli enable <job>      # Enable a job
jenkins-cli disable <job>     # Disable a job
jenkins-cli delete <job>      # Delete a job
```

## Config

Priority: `~/.jenkinscli/config` > env vars > defaults

| Key | Env Var | Default |
|-----|---------|---------|
| `JENKINS_API` | `JENKINS_API` | `http://127.0.0.1:8080` |
| `JENKINS_USER` | `JENKINS_USER` | `root` |
| `JENKINS_PASSWORD` | `JENKINS_PASSWORD` | `dangerous` |
| `CONFIG_DIR` | `CONFIG_DIR` | `config/*.yaml` |

See [config.example](./config.example) for a template.

## YAML Job Format

```yaml
# Git-triggered job
jobs:
  - name: my-job
    config:
      git-url: "git@github.com:user/repo.git"
      git-branch: "origin/main"
      assignedNode: "master"
      build-script: |
        #!/bin/bash
        echo "Hello World"

# Cron job
  - name: my-cron
    config:
      cron: "H/5 * * * *"
      build-script: |
        #!/bin/bash
        echo "Scheduled task"
```

## Build from Source

```bash
git clone https://github.com/zgfh/jenkins-cli.git
cd jenkins-cli
make build          # local platform
make build-linux    # cross-compile for Linux
```
