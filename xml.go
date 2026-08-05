package main

import "strings"

const gitJobXML = `<?xml version='1.1' encoding='UTF-8'?>
<project>
  <actions/>
  <description></description>
  <keepDependencies>false</keepDependencies>
  <properties>
    <hudson.plugins.jira.JiraProjectProperty plugin="jira@3.0.8"/>
  </properties>
  <scm class="hudson.plugins.git.GitSCM" plugin="git@3.10.0">
    <configVersion>2</configVersion>
    <userRemoteConfigs>
      <hudson.plugins.git.UserRemoteConfig>
        <url>__GIT_URL__</url>
        <credentialsId>__GIT_AUTH_ID__</credentialsId>
      </hudson.plugins.git.UserRemoteConfig>
    </userRemoteConfigs>
    <branches>
      <hudson.plugins.git.BranchSpec>
        <name>*/__GIT_BRANCH__</name>
      </hudson.plugins.git.BranchSpec>
    </branches>
    <doGenerateSubmoduleConfigurations>false</doGenerateSubmoduleConfigurations>
    <submoduleCfg class="list"/>
    <extensions/>
  </scm>
  __CAN_ROAM__
  <disabled>false</disabled>
  <blockBuildWhenDownstreamBuilding>false</blockBuildWhenDownstreamBuilding>
  <blockBuildWhenUpstreamBuilding>false</blockBuildWhenUpstreamBuilding>
  <triggers>
    <hudson.triggers.SCMTrigger>
      <spec>TZ=Asia/Shanghai

* * * * *</spec>
      <ignorePostCommitHooks>false</ignorePostCommitHooks>
    </hudson.triggers.SCMTrigger>
  </triggers>
  <concurrentBuild>false</concurrentBuild>
  <builders>
    <hudson.tasks.Shell>
      <command>__BUILD_SCRIPT__</command>
    </hudson.tasks.Shell>
  </builders>
  <publishers/>
  <buildWrappers/>
</project>`

const cronJobXML = `<?xml version='1.1' encoding='UTF-8'?>
<project>
  <actions/>
  <description></description>
  <keepDependencies>false</keepDependencies>
  <properties/>
  <scm class="hudson.scm.NullSCM"/>
  __CAN_ROAM__
  <disabled>false</disabled>
  <blockBuildWhenDownstreamBuilding>false</blockBuildWhenDownstreamBuilding>
  <blockBuildWhenUpstreamBuilding>false</blockBuildWhenUpstreamBuilding>
  <triggers>
    <hudson.triggers.TimerTrigger>
      <spec>TZ=Asia/Shanghai

__CRON__</spec>
    </hudson.triggers.TimerTrigger>
  </triggers>
  <concurrentBuild>false</concurrentBuild>
  <builders>
    <hudson.tasks.Shell>
      <command>__BUILD_SCRIPT__</command>
    </hudson.tasks.Shell>
  </builders>
  <publishers/>
  <buildWrappers/>
</project>`

func generateJobXML(j Job) string {
	var xml string
	if j.IsGitJob() {
		xml = gitJobXML
		authID := j.Config.GitAuthID
		if authID == "" {
			authID = "token"
		}
		xml = strings.ReplaceAll(xml, "__GIT_URL__", j.Config.GitURL)
		xml = strings.ReplaceAll(xml, "__GIT_AUTH_ID__", authID)
		xml = strings.ReplaceAll(xml, "__GIT_BRANCH__", j.Config.GitBranch)
	} else {
		xml = cronJobXML
		cron := j.Config.Cron
		if cron == "" {
			cron = "H/5 * * * *"
		}
		xml = strings.ReplaceAll(xml, "__CRON__", cron)
	}

	xml = strings.ReplaceAll(xml, "__BUILD_SCRIPT__", j.Config.BuildScript)

	disabled := "false"
	if !j.IsEnabled() {
		disabled = "true"
	}
	xml = strings.ReplaceAll(xml, "<disabled>false</disabled>", "<disabled>"+disabled+"</disabled>")

	if j.Config.AssignedNode != "" {
		xml = strings.ReplaceAll(xml, "__CAN_ROAM__", "<canRoam>false</canRoam>\n  <assignedNode>"+j.Config.AssignedNode+"</assignedNode>")
	} else {
		xml = strings.ReplaceAll(xml, "__CAN_ROAM__", "<canRoam>true</canRoam>")
	}

	return xml
}
