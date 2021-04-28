package main

import (
	"encoding/json"
	"fmt"
	"github.com/xanzy/go-gitlab"
	"io/ioutil"
	"strings"
)

type Config struct {
	GitlabToken              string            `json:"gitlab.token"`
	GitlabLabelToJiraProject map[string]string `json:"gitlabLabelToJiraProject"`
	GitlabRepoToJiraProject  map[string]string `json:"gitlabRepoToJiraProject"`
}

type JiraIssueComment struct {
	Body string `json:"body"`
}

type JiraIssue struct {
	IssueType   string             `json:"issueType"`
	Status      string             `json:"status"`
	Summary     string             `json:"summary"`
	Description string             `json:"description"`
	Labels      []string           `json:"labels,omitempty"`
	Comments    []JiraIssueComment `json:"comments,omitempty"`
}

type JiraProject struct {
	Key    string      `json:"key"`
	Issues []JiraIssue `json:"issues"`
}

type JiraExport struct {
	Projects []JiraProject `json:"projects"`
}

func main() {
	// read config
	configBytes, err := ioutil.ReadFile("config.json")
	if err != nil {
		panic(err)
	}
	var config Config
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		panic(err)
	}

	// init gitlab client
	git, err := gitlab.NewClient(config.GitlabToken)
	if err != nil {
		panic(err)
	}

	// get all issues
	var issues []*gitlab.Issue
	pageNum := 1
	for true {
		opts := &gitlab.ListGroupIssuesOptions{
			ListOptions: gitlab.ListOptions{
				Page:    pageNum,
				PerPage: 10,
			},
		}
		issuesOnPage, _, err := git.Issues.ListGroupIssues("k-t-corp", opts)
		if err != nil {
			panic(err)
		}
		if len(issuesOnPage) == 0 {
			break
		}
		for _, i := range issuesOnPage {
			issues = append(issues, i)
		}
		pageNum += 1
	}

	// reverse issues so that earliest comes first
	for i, j := 0, len(issues)-1; i < j; i, j = i+1, j-1 {
		issues[i], issues[j] = issues[j], issues[i]
	}

	projectIssues := make(map[string][]JiraIssue)
	for _, issue := range issues {
		fmt.Println("Examining '" + issue.Title + "'")

		// determine the jira project to go by gitlab labels
		jiraProject := ""
		for _, l := range issue.Labels {
			if jp, ok := config.GitlabLabelToJiraProject[l]; ok {
				jiraProject = jp
				break
			}
		}
		if jiraProject == "" {
			// determine the jira project to go by gitlab project
			gitlabProject, _, err := git.Projects.GetProject(issue.ProjectID, &gitlab.GetProjectOptions{})
			if err != nil {
				panic(err)
			}
			if jp, ok := config.GitlabRepoToJiraProject[gitlabProject.PathWithNamespace]; ok {
				jiraProject = jp
			}
		}

		if jiraProject == "" {
			panic(">> This issue does not belong to any Jira project")
		}

		// determine jira issue type by (absent of) gitlab labels
		// TODO: can remove hardcodes
		jiraIssueType := "Story"
		if jiraProject == "ENG" {
			jiraIssueType = "Epic"
		} else {
			for _, l := range issue.Labels {
				if l == "bug" {
					jiraIssueType = "Bug"
					break
				}
			}
		}

		// determine jira issue status by (absent of) gitlab labels
		// TODO: can remove hardcodes
		jiraIssueStatus := "To Do"
		isWontFix := false
		for _, l := range issue.Labels {
			if l == "status:wont-fix" {
				isWontFix = true
				break
			}
		}
		if issue.State == "closed" && !isWontFix {
			jiraIssueStatus = "Done"
		}

		// determine extra jira labels
		// TODO: can remove hardcodes
		var jiraIssueLabels []string
		for _, l := range issue.Labels {
			if l == "cost" || l == "monetize" {
				jiraIssueLabels = append(jiraIssueLabels, l)
			} else if strings.HasPrefix(l, "topic::") {
				jiraIssueLabels = append(jiraIssueLabels, l[len("topic::"):])
			}
		}

		// construct comments
		discussions, _, err := git.Discussions.ListIssueDiscussions(issue.ProjectID, issue.IID, &gitlab.ListIssueDiscussionsOptions{})
		if err != nil {
			panic(err)
		}
		var jiraIssueComments []JiraIssueComment
		for _, d := range discussions {
			for _, n := range d.Notes {
				if !n.System {
					jiraIssueComments = append(jiraIssueComments, JiraIssueComment{
						Body: n.Body,
					})
				}
			}
		}
		jiraIssueComments = append(jiraIssueComments, JiraIssueComment{
			Body: fmt.Sprintf("Imported from %s", issue.WebURL),
		})

		// construct issue
		jiraIssue := JiraIssue{
			IssueType:   jiraIssueType,
			Status:      jiraIssueStatus,
			Summary:     issue.Title,
			Description: issue.Description,
			Labels:      jiraIssueLabels,
			Comments:    jiraIssueComments,
		}
		if _, ok := projectIssues[jiraProject]; !ok {
			projectIssues[jiraProject] = []JiraIssue{}
		}
		projectIssues[jiraProject] = append(projectIssues[jiraProject], jiraIssue)
	}

	// export
	var projects []JiraProject
	for p, ii := range projectIssues {
		projects = append(projects, JiraProject{
			Key:    p,
			Issues: ii,
		})
	}
	export := JiraExport{
		Projects: projects,
	}

	// write export
	bytes, err := json.MarshalIndent(export, "", "    ")
	if err != nil {
		panic(err)
	}
	err = ioutil.WriteFile("export.json", bytes, 0644)
	if err != nil {
		panic(err)
	}
}
