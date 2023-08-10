package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	units "github.com/docker/go-units"

	"github.com/google/go-github/v50/github"
	"golang.org/x/exp/slices"
	"golang.org/x/oauth2"
)

type RepoSummary struct {
	Counts            map[string]int
	Runs              int
	Jobs              int
	Workflows         int
	TotalTime         time.Duration
	TotalBillableTime time.Duration
	LongestBuild      time.Duration
	Name              string
	FullName          string
}

type TeamSummary struct {
	Counts            map[string]int
	Runs              int
	Jobs              int
	Workflows         int
	TotalTime         time.Duration
	TotalBillableTime time.Duration
	LongestBuild      time.Duration
	Name              string
	Slug              string
}

type Team struct {
	Name       string     `json:"name"`
	Slug       string     `json:"slug"`
	Maintainer string     `json:"maintainer"`
	Repos      []TeamRepo `json:"repos"`
}

type TeamRepo struct {
	Name     string `json:"name"`
	FullName string `json:"fullname"`
}

type JsonData struct {
	Entity        string
	Days          int
	TotalRuns     int
	TotalJobs     int
	TotalPrivate  int
	TotalPublic   int
	Actors        map[string]bool
	Conclusion    map[string]int
	Repos         []*github.Repository
	AllRepos      []*github.Repository
	AllUsageRepos []*UsageRepository
	RepoSummary   []*RepoSummary
	StartDate     string
	EndDate       string
	AllUsage      time.Duration
	AllBill       time.Duration
}

type UsageUser struct {
	Id    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

// WorkflowRun represents a repository action workflow run.
type UsageWorkflowRun struct {
	ID           int64         `json:"id,omitempty"`
	Name         string        `json:"name,omitempty"`
	HeadBranch   string        `json:"head_branch,omitempty"`
	HeadSHA      string        `json:"head_sha,omitempty"`
	RunNumber    int           `json:"run_number,omitempty"`
	RunAttempt   int           `json:"run_attempt,omitempty"`
	Event        string        `json:"event,omitempty"`
	Status       string        `json:"status,omitempty"`
	Conclusion   string        `json:"conclusion,omitempty"`
	WorkflowID   int64         `json:"workflow_id,omitempty"`
	CreatedAt    time.Time     `json:"created_at,omitempty"`
	UpdatedAt    time.Time     `json:"updated_at,omitempty"`
	RunStartedAt time.Time     `json:"run_started_at,omitempty"`
	Actor        UsageUser     `json:"actor,omitempty"`
	RunDuration  time.Duration `json:"run_duration,omitempty"`
	Billable     time.Duration `json:"billable,omitempty"`
}

type UsageRepository struct {
	Id           int64               `json:"id"`
	Name         string              `json:"name"`
	FullName     string              `json:"full_name"`
	Visibility   string              `json:"visibility"`
	Owner        *UsageUser          `json:"owner"`
	Workflows    []*UsageWorkflow    `json:"workflows"`
	WorkflowRuns []*UsageWorkflowRun `json:"workflow_runs"`
}

type ubuntuBillable struct {
	UBUNTU struct {
		TotalMs int `json:"total_ms"`
		Jobs    int `json:"jobs"`
		JobRuns []struct {
			JobID      int64 `json:"job_id"`
			DurationMs int   `json:"duration_ms"`
		} `json:"job_runs"`
	} `json:"UBUNTU"`
}
type WorkflowBillMap map[string]*WorkflowBill

type WorkflowBill struct {
	TotalMS int64 `json:"total_ms,omitempty"`
}

type UsageWorkflow struct {
	ID       int64                   `json:"id,omitempty"`
	NodeID   string                  `json:"node_id,omitempty"`
	Name     string                  `json:"name,omitempty"`
	Path     string                  `json:"path,omitempty"`
	State    string                  `json:"state,omitempty"`
	URL      string                  `json:"url,omitempty"`
	HTMLURL  string                  `json:"html_url,omitempty"`
	BadgeURL string                  `json:"badge_url,omitempty"`
	Billable *github.WorkflowBillMap `json:"billable,omitempty"`
}

func main() {

	var (
		orgName, userName, token, tokenFile, fromFile, repoName string
		days                                                    int
		byRepo, byTeam                                          bool
	)

	flag.StringVar(&orgName, "org", "", "Organization name")
	flag.StringVar(&userName, "user", "", "User name")
	flag.StringVar(&token, "token", "", "GitHub token")
	flag.StringVar(&tokenFile, "token-file", "", "Path to the file containing the GitHub token")
	flag.StringVar(&fromFile, "from-file", "", "Path to the json file to process")
	flag.StringVar(&repoName, "repo", "", "repo to view detail")

	flag.BoolVar(&byRepo, "by-repo", false, "Show breakdown by repository")
	flag.BoolVar(&byTeam, "by-team", false, "Show breakdown by team")

	// flag.BoolVar(&punchCard, "punch-card", false, "Show punch card with breakdown of builds per day")
	// flag.IntVar(&days, "days", 30, "How many days of data to query from the GitHub API")

	// flag.IntVar(&threadhold, "days", 30, "How many days of data to query from the GitHub API")

	flag.Parse()

	// if fromFile == "" {
	// 	panic("blank1")
	// }

	// if strings.TrimSpace(fromFile) == "" {
	// 	panic("blank")
	// }
	// panic("ss")

	if fromFile == "" && tokenFile != "" {
		tokenBytes, err := os.ReadFile(tokenFile)
		if err != nil {
			log.Fatal(err)
		}
		token = strings.TrimSpace(string(tokenBytes))
	}

	if fromFile == "" && token == "" {
		log.Fatal("token is required")

	}

	auth := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))

	switch {
	case orgName == "" && userName == "" && fromFile == "":
		log.Fatal("Organization name or username or fromFile is required")
	case orgName != "" && userName != "" && fromFile == "":
		log.Fatal("Only org or username must be specified at the same time")
	}

	client := github.NewClient(auth)
	today := time.Now()
	days = today.Day()
	created := today.AddDate(0, 0, -days+1)
	format := "2006-01-02"
	createdQuery := ">=" + created.Format(format)

	var (
		entity       string
		totalRuns    int
		totalJobs    int
		totalPrivate int
		totalPublic  int
		allUsage     time.Duration
		allBill      time.Duration
		// longestBuild time.Duration
		actors     map[string]bool
		conclusion map[string]int
		// buildsPerDay map[string]int
		repoSummary map[string]*RepoSummary
		teamSummary map[string]*TeamSummary
	)

	repoSummary = make(map[string]*RepoSummary)
	teamSummary = make(map[string]*TeamSummary)

	actors = make(map[string]bool)
	// buildsPerDay = map[string]int{
	// 	"Monday":    0,
	// 	"Tuesday":   0,
	// 	"Wednesday": 0,
	// 	"Thursday":  0,
	// 	"Friday":    0,
	// 	"Saturday":  0,
	// 	"Sunday":    0,
	// }

	conclusion = map[string]int{
		"success":   0,
		"failure":   0,
		"cancelled": 0,
		"skipped":   0,
	}

	var repos, allRepos []*github.Repository
	var allUsageRepos []*UsageRepository
	var summaries []*RepoSummary
	var allTeams = []*Team{}
	var res *github.Response
	var err error
	var rateLimit *github.RateLimits
	// var storageBilling *github.StorageBilling
	ctx := context.Background()
	page := 0

	allUsage = time.Second * 0
	allBill = time.Second * 0

	var owner string
	if orgName != "" {
		owner = orgName
	}
	if userName != "" {
		owner = userName
	}

	// Fetching online
	if fromFile == "" {
		jsonBytes, errTeamsCache := os.ReadFile("allTeams.json")
		if errTeamsCache != nil {
			fmt.Println("Can not read teams data from cache")

		} else {
			json.Unmarshal(jsonBytes, &allTeams)
		}

		// Fetching from github api
		fmt.Printf("Fetching last %d days of data (created>=%s)\n", days, created.Format("2006-01-02"))
		rateLimit, _, err = client.RateLimits(ctx)
		if err != nil {
			log.Fatal(err)
		}
		// log.Printf("***")
		log.Printf("Rate Limit Remain %d\n", rateLimit.Core.Remaining)

		if repoName != "" && !byRepo && !byTeam {
			var repo *github.Repository
			if orgName != "" {
				repo, res, err = client.Repositories.Get(ctx, orgName, repoName)
				// log.Printf("***")
				if err != nil {
					log.Fatal(err)
				}

			} else if userName != "" {
				repo, res, err = client.Repositories.Get(ctx, userName, repoName)
				// log.Printf("***")
				if err != nil {
					log.Fatal(err)
				}

			}

			record, _ := json.Marshal(repo)
			var usageRepo UsageRepository
			json.Unmarshal([]byte(record), &usageRepo)
			allUsageRepos = append(allUsageRepos, &usageRepo)
			allRepos = append(allRepos, repo)

		} else {
			for {
				log.Printf("Fetching repos %s page %d", orgName, page)
				if orgName != "" {
					opts := &github.RepositoryListByOrgOptions{ListOptions: github.ListOptions{Page: page, PerPage: 100}, Type: "all"}
					repos, res, err = client.Repositories.ListByOrg(ctx, orgName, opts)
					// log.Printf("***")

				} else if userName != "" {
					opts := &github.RepositoryListOptions{ListOptions: github.ListOptions{Page: page, PerPage: 100}, Type: "all"}
					repos, res, err = client.Repositories.List(ctx, userName, opts)
					// log.Printf("***")

				}

				if err != nil {
					log.Fatal(err)
				}
				if res.Rate.Remaining == 0 {
					panic("Rate limit exceeded")
				}

				// var usageRepos []*UsageRepository
				for _, repo := range repos {
					record, _ := json.Marshal(repo)
					var usageRepo UsageRepository
					json.Unmarshal([]byte(record), &usageRepo)
					allUsageRepos = append(allUsageRepos, &usageRepo)
				}

				allRepos = append(allRepos, repos...)

				log.Printf("Status: %d Page %d, next page: %d", res.StatusCode, page, res.NextPage)

				if len(allRepos) == 0 {
					break
				}
				if res.NextPage == 0 {
					break
				}

				// break
				page = res.NextPage
			}
		}

		for i, repo := range allRepos {
			log.Printf("Found: %s", repo.GetFullName())
			if repo.GetPrivate() {
				totalPrivate++
			} else {
				totalPublic++
			}

			// Try to get repo team from cache
			repoTeam := getTeamFromCache(allTeams, repo.GetName())

			var reportTeam = repoTeam.Name
			var reportSlug = repoTeam.Slug
			var maintainTeam = ""
			var maintainSlug = ""
			var writeTeam = ""
			var writeSlug = ""

			if repoTeam.Name == "N/a" {
				reportTeam = "Other"
				reportSlug = "other"
				var teams []*github.Team
				opts := &github.ListOptions{Page: page, PerPage: 100}

				teams, res, err = client.Repositories.ListTeams(ctx, orgName, repo.GetName(), opts)
				// log.Printf("***")

				// fmt.Printf("%+v\n", teams)
				for _, team := range teams {
					log.Printf("Team: %s, Permission: %s", team.GetName(), team.GetPermission())
					if team.GetPermission() == "admin" {
						reportTeam = team.GetName()
						reportSlug = team.GetSlug()
					} else if team.GetPermission() == "maintain" {
						maintainTeam = team.GetName()
						maintainSlug = team.GetSlug()

					} else if team.GetPermission() == "push" {
						writeTeam = team.GetName()
						writeSlug = team.GetSlug()
					}
				}

				if reportTeam == "Other" && maintainTeam != "" {
					reportTeam = maintainTeam
					reportSlug = maintainSlug
				} else if reportTeam == "Other" && writeTeam != "" {
					reportTeam = writeTeam
					reportSlug = writeSlug
				}

			}

			log.Printf("Add stat to Team: %s", reportTeam)

			var rp = TeamRepo{
				Name:     repo.GetName(),
				FullName: repo.GetFullName(),
			}
			// fmt.Printf("%+v\n", rp)

			idx := slices.IndexFunc(allTeams, func(t *Team) bool { return t.Name == reportTeam })
			if idx > -1 {
				log.Printf("Team: %s FOUND in cache\n", reportTeam)
				idx2 := slices.IndexFunc(allTeams[idx].Repos, func(tr TeamRepo) bool { return tr.Name == rp.Name })
				if idx2 == -1 {
					log.Printf("Add repo: %s to Team: %s\n", rp.Name, reportTeam)
					allTeams[idx].Repos = append(allTeams[idx].Repos, rp)
					// log.Printf("%+v\n", allTeams[idx].Repos)
				} else {
					log.Printf("Repos %s Already exist in Team: %s\n", rp.Name, reportTeam)
					// log.Printf("%+v\n", allTeams[idx].Repos)

				}

			} else {
				log.Printf("Team: %s NotFound in cache creating\n", reportTeam)
				log.Printf("Add repo: %s to Team: %s\n", rp.Name, reportTeam)

				var users []*github.User
				listuseropts := &github.TeamListTeamMembersOptions{Role: "maintainer", ListOptions: github.ListOptions{Page: page, PerPage: 100}}

				var team = &Team{
					Name:       reportTeam,
					Slug:       reportSlug,
					Maintainer: "N/a",
					Repos: []TeamRepo{
						rp,
					},
				}

				users, _, err = client.Teams.ListTeamMembersBySlug(ctx, orgName, reportTeam, listuseropts)
				// log.Printf("***")

				if err != nil {
					// panic
					team.Maintainer = "N/a"
				}

				if len(users) > 0 {
					team.Maintainer = users[0].GetLogin()

				}

				allTeams = append(allTeams, team)
			}

			// ########################################################

			//Create summary for repo
			if _, ok := repoSummary[repo.GetFullName()]; !ok {
				repoSummary[repo.GetFullName()] = &RepoSummary{
					Counts:            make(map[string]int),
					TotalTime:         time.Second * 0,
					TotalBillableTime: time.Second * 0,
					Name:              repo.GetName(),
					FullName:          repo.GetFullName(),
				}
			}
			summary := repoSummary[owner+"/"+repo.GetName()]

			// workflows := []*UsageWorkflow{}
			page := 0
			for {
				log.Printf("Listing workflows for: %s", repo.GetFullName())
				var wkflow *github.Workflows
				opts := &github.ListOptions{Page: page, PerPage: 100}
				if orgName != "" {
					wkflow, res, err = client.Actions.ListWorkflows(ctx, orgName, repo.GetName(), opts)
					// log.Printf("***")

				}
				if userName != "" {
					realOwner := userName
					// if user is a member of repository
					if userName != *repo.Owner.Login {
						realOwner = *repo.Owner.Login
					}
					wkflow, res, err = client.Actions.ListWorkflows(ctx, realOwner, repo.GetName(), opts)
					// log.Printf("***")

				}
				log.Printf("Found %d workflows for %s", len(wkflow.Workflows), repo.GetFullName())
				summary.Workflows = wkflow.GetTotalCount()

				for _, w := range wkflow.Workflows {
					var wflow *UsageWorkflow
					jsonData, _ := json.Marshal(w)
					err = json.Unmarshal(jsonData, &wflow)
					if err != nil {
						log.Fatal(err)
					}
					allUsageRepos[i].Workflows = append(allUsageRepos[i].Workflows, wflow)

				}

				if res.NextPage == 0 {
					break
				}
			}

			workflowRuns := []*github.WorkflowRun{}
			var workflowsToGetUsage []int64
			if len(allUsageRepos[i].Workflows) > 0 {
				page = 0
				for {

					opts := &github.ListWorkflowRunsOptions{Created: createdQuery, ListOptions: github.ListOptions{Page: page, PerPage: 100}}

					var runs *github.WorkflowRuns
					log.Printf("Listing workflow runs for: %s", repo.GetFullName())
					if orgName != "" {
						runs, res, err = client.Actions.ListRepositoryWorkflowRuns(ctx, orgName, repo.GetName(), opts)
						// log.Printf("***")

					}
					if userName != "" {
						realOwner := userName
						// if user is a member of repository
						if userName != *repo.Owner.Login {
							realOwner = *repo.Owner.Login
						}
						runs, res, err = client.Actions.ListRepositoryWorkflowRuns(ctx, realOwner, repo.GetName(), opts)
						// log.Printf("***")

					}

					if err != nil {
						log.Fatal(err)
					}

					workflowRuns = append(workflowRuns, runs.WorkflowRuns...)

					if len(workflowRuns) == 0 {
						break
					}

					if res.NextPage == 0 {
						break
					}

					page = res.NextPage
				}

				log.Printf("Found %d workflow runs for %s/%s", len(workflowRuns), owner, repo.GetName())
				summary.Runs += len(workflowRuns)

				for _, run := range workflowRuns {

					if a := run.GetActor(); a != nil {
						actors[a.GetLogin()] = true
					}

					conclusion[run.GetConclusion()]++
					summary.Counts[run.GetConclusion()]++

					record, _ := json.Marshal(run)
					var usageWorkflow UsageWorkflowRun
					json.Unmarshal([]byte(record), &usageWorkflow)
					allUsageRepos[i].WorkflowRuns = append(allUsageRepos[i].WorkflowRuns, &usageWorkflow)

					idx := slices.IndexFunc(workflowsToGetUsage, func(w int64) bool { return w == usageWorkflow.WorkflowID })
					if idx == -1 {
						workflowsToGetUsage = append(workflowsToGetUsage, usageWorkflow.WorkflowID)

					}
				}

				totalRuns += len(workflowRuns)
			}

			// Get usage only workflow that have run
			for _, w := range workflowsToGetUsage {

				var workflowUsage *github.WorkflowUsage

				workflowUsage, res, _ = client.Actions.GetWorkflowUsageByID(ctx, orgName, repo.GetName(), w)
				// log.Printf("***")

				idx := slices.IndexFunc(allUsageRepos[i].Workflows, func(uw *UsageWorkflow) bool { return uw.ID == w })
				if idx != -1 {
					allUsageRepos[i].Workflows[idx].Billable = workflowUsage.GetBillable()
				}

				var billable ubuntuBillable

				billableJson, _ := json.Marshal(workflowUsage.GetBillable())
				err = json.Unmarshal(billableJson, &billable)
				if err != nil {
					log.Fatal(err)
				}
				// fmt.Printf("%+v\n", billable)
				log.Printf("[%s] - Billable TotalMs: %d", allUsageRepos[i].Workflows[idx].Name, billable.UBUNTU.TotalMs)
				// fmt.Printf("%+v\n", summary)

				summary.TotalBillableTime += time.Duration(billable.UBUNTU.TotalMs) * time.Millisecond

			}

		}
		// panic("test")

		entity = orgName
		if orgName == "" {
			entity = userName
		}

		// summaries := []*RepoSummary{}

		for _, summary := range repoSummary {
			summaries = append(summaries, summary)
		}

		enddate := time.Now()

		data := JsonData{
			TotalRuns:    totalRuns,
			TotalJobs:    totalJobs,
			TotalPrivate: totalPrivate,
			TotalPublic:  totalPublic,
			// AllRepos:        allRepos,
			AllUsageRepos: allUsageRepos,
			RepoSummary:   summaries,
			Actors:        actors,
			Conclusion:    conclusion,
			Entity:        entity,
			Days:          days,
			StartDate:     created.Format(format),
			EndDate:       enddate.Format(format),
			AllUsage:      allUsage,
		}

		jsonData, err := json.MarshalIndent(data, "", "  ")

		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		} else {
			// fmt.Println(string(jsonData))
			_ = os.WriteFile("cache.json", jsonData, 0644)

		}

		jsonData, err = json.MarshalIndent(allTeams, "", "  ")

		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		} else {
			_ = os.WriteFile("allTeams.json", jsonData, 0644)

		}

	} else if fromFile != "" {

		// if repoName != "" {
		// 	log.Printf("can not get repo detail in file mode")
		// 	os.Exit(1)

		// }

		// Read data from file
		jsonBytes, err := os.ReadFile(fromFile)
		if err != nil {
			log.Fatal(err)
		}
		var data JsonData

		err = json.Unmarshal(jsonBytes, &data)
		if err != nil {
			// panic
			log.Fatal(err)
		}
		summaries = data.RepoSummary
		entity = data.Entity
		days = data.Days
		totalRuns = data.TotalRuns
		// totalJobs = data.TotalJobs
		totalPrivate = data.TotalPrivate
		totalPublic = data.TotalPublic
		// allRepos = data.AllRepos
		allUsageRepos = data.AllUsageRepos
		conclusion = data.Conclusion
		// teamMaintainers = data.TeamMaintainers
		actors = data.Actors
		allUsage = data.AllUsage

		jsonBytes, err = os.ReadFile("allTeams.json")
		if err != nil {
			fmt.Println("Can not read teams data from cache")

		} else {
			json.Unmarshal(jsonBytes, &allTeams)
		}

		log.Printf("Reading data from file: %s \n", fromFile)
		fmt.Printf("Fetching last %d days of data (created>=%s - %s)\n", days, data.StartDate, data.EndDate)

		if repoName != "" && !byRepo && !byTeam {
			fmt.Printf("Fetching data for repo: %s \n", repoName)

			var tempUsageRepos []*UsageRepository
			idx := slices.IndexFunc(allUsageRepos, func(us *UsageRepository) bool { return us.Name == repoName })
			if idx > -1 {
				tempUsageRepos = append(tempUsageRepos, allUsageRepos[idx])
				allUsageRepos = tempUsageRepos
			} else {
				fmt.Printf("NotFound repo: %s\n", repoName)
				os.Exit(4)
			}

			var tempsummaries []*RepoSummary
			idx = slices.IndexFunc(summaries, func(r *RepoSummary) bool { return r.Name == repoName })
			if idx > -1 {
				// fmt.Printf("Fetching data for repo: %s \n", repoName)

				tempsummaries = append(tempsummaries, summaries[idx])

			}
			summaries = tempsummaries
		}

	}

	// fmt.Printf("\nGenerated by: kd-actions-usage\nReport for %s - last: %d days.\n\n", entity, days)
	fmt.Printf("\nReport for: %s\nlast: %d days.\n\n", entity, days)
	if fromFile == "" {
		fmt.Printf("Total repos: %d\n", len(allRepos))

	} else {
		fmt.Printf("Total repos: %d\n", len(allUsageRepos))
	}

	if repoName == "" {
		fmt.Printf("Total private repos: %d\n", totalPrivate)
		fmt.Printf("Total public repos: %d\n", totalPublic)
		fmt.Println()
		fmt.Printf("Total workflow runs: %d\n", totalRuns)
		fmt.Println()
		fmt.Printf("Total users: %d\n", len(actors))
	} else {
		fmt.Printf("Repo name: %s\n", repoName)

	}

	if totalRuns > 0 && repoName == "" {
		fmt.Println()
		fmt.Printf("Success: %d/%d\n", conclusion["success"], totalRuns)
		fmt.Printf("Failure: %d/%d\n", conclusion["failure"], totalRuns)
		fmt.Printf("Cancelled: %d/%d\n", conclusion["cancelled"], totalRuns)
		if conclusion["skipped"] > 0 {
			fmt.Printf("Skipped: %d/%d\n", conclusion["skipped"], totalRuns)
		}
		fmt.Println()
		// fmt.Printf("Longest build: %s\n", longestBuild.Round(time.Second))
		// fmt.Printf("Average build time: %s\n", (allUsage / time.Duration(totalJobs)).Round(time.Second))

		fmt.Println()

	}

	if byRepo {
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 4, 4, 1, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "Repo\tTeam\tRuns\tSuccess\tFailure\tCancelled\tBill\tAverage")
		fmt.Fprintln(w, "----\t----\t----\t-------\t-------\t---------\t----\t-------")
		// fmt.Fprintln(w, "Repo\tTeam\tWorkflows\tBill")

		sort.Slice(summaries, func(i, j int) bool {
			// if summaries[i].Runs == summaries[j].Runs {
			// 	return summaries[i].TotalBillableTime > summaries[j].TotalBillableTime
			// }

			return summaries[i].TotalBillableTime > summaries[j].TotalBillableTime
		})
		allBill = 0
		for _, summary := range summaries {
			var repoTeam = "Other"
			idx := slices.IndexFunc(allTeams, func(t *Team) bool {
				idx2 := slices.IndexFunc(t.Repos, func(r TeamRepo) bool {
					return r.Name == summary.Name
				})
				return idx2 > -1

			})
			if idx > -1 {
				repoTeam = allTeams[idx].Name
			}

			avg := time.Duration(0)
			if summary.TotalBillableTime > 0 {
				avg = summary.TotalBillableTime / time.Duration(summary.Runs)

				fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
					summary.Name,
					repoTeam,
					summary.Runs,
					summary.Counts["success"],
					summary.Counts["failure"],
					summary.Counts["cancelled"],
					summary.TotalBillableTime,
					avg.Round(time.Second),
				)
			}

			allBill += summary.TotalBillableTime

		}

		w.Flush()

	} else if byTeam {
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 4, 4, 1, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "Team\tMaintainer\tRuns\tSuccess\tFailure\tCancelled\tBill\tAverage")
		fmt.Fprintln(w, "----\t----------\t----\t-------\t-------\t---------\t----\t-------")

		sort.Slice(summaries, func(i, j int) bool {
			// if summaries[i].Runs == summaries[j].Runs {
			// 	return summaries[i].TotalBillableTime > summaries[j].TotalBillableTime
			// }

			return summaries[i].TotalBillableTime > summaries[j].TotalBillableTime
		})

		for _, summary := range summaries {

			var repoTeam = "Other"
			var repoTeamSlug = "other"
			idx := slices.IndexFunc(allTeams, func(t *Team) bool {
				idx2 := slices.IndexFunc(t.Repos, func(r TeamRepo) bool {
					return r.Name == summary.Name
				})
				return idx2 > -1

			})
			if idx > -1 {
				repoTeam = allTeams[idx].Name
				repoTeamSlug = allTeams[idx].Slug
			}

			if _, ok := teamSummary[repoTeamSlug]; !ok {
				teamSummary[repoTeamSlug] = &TeamSummary{
					Counts:       make(map[string]int),
					TotalTime:    time.Second * 0,
					Name:         repoTeam,
					Slug:         repoTeamSlug,
					LongestBuild: time.Second * 0,
				}
			}

			teamsummary := teamSummary[repoTeamSlug]
			teamsummary.TotalTime += summary.TotalTime
			teamsummary.TotalBillableTime += summary.TotalBillableTime
			teamsummary.Runs += summary.Runs
			teamsummary.Counts["success"] += summary.Counts["success"]
			teamsummary.Counts["failure"] += summary.Counts["failure"]
			teamsummary.Counts["cancelled"] += summary.Counts["cancelled"]
			if summary.LongestBuild > teamsummary.LongestBuild {
				teamsummary.LongestBuild = summary.LongestBuild
			}

		}
		allBill = 0
		for _, summary := range teamSummary {

			var maintainer = ""
			idx := slices.IndexFunc(allTeams, func(t *Team) bool { return t.Name == summary.Name })
			if idx > -1 {
				maintainer = allTeams[idx].Maintainer
			}
			avg := time.Duration(0)
			if summary.Runs > 0 {
				avg = summary.TotalBillableTime / time.Duration(summary.Runs)

				fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
					summary.Name,
					maintainer,
					summary.Runs,
					summary.Counts["success"],
					summary.Counts["failure"],
					summary.Counts["cancelled"],
					summary.TotalBillableTime.Round(time.Second),
					avg.Round(time.Second),
					// summary.LongestBuild.Round(time.Second)
				)
			}

			allBill += summary.TotalBillableTime

		}

		w.Flush()

	} else if repoName != "" {
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 4, 4, 1, ' ', tabwriter.TabIndent)
		// fmt.Fprintln(w, "Workflow\tRuns\tSuccess\tFailure\tCancelled\tTotal\tBill\tAverage\tLongest")
		fmt.Fprintln(w, "Workflow\tRuns\tSuccess\tFailure\tCancelled\tBill")
		fmt.Fprintln(w, "--------\t----\t-------\t-------\t---------\t----")

		workflows := allUsageRepos[0].Workflows
		workflow_runs := allUsageRepos[0].WorkflowRuns
		allBill = 0

		for _, wf := range workflows {
			var wkconclusion = map[string]int{
				"success":   0,
				"failure":   0,
				"cancelled": 0,
				"total":     0,
			}

			for _, r := range workflow_runs {
				if r.WorkflowID == wf.ID {
					wkconclusion[r.Conclusion] += 1
					wkconclusion["total"] += 1
				}
			}

			var billable ubuntuBillable

			billableJson, _ := json.Marshal(wf.Billable)
			err = json.Unmarshal(billableJson, &billable)
			if err != nil {
				log.Fatal(err)
			}
			// fmt.Printf("%+v\n", billable)
			// log.Printf("Billable TotalMs: %d", billable.UBUNTU.TotalMs)
			bill := time.Duration(billable.UBUNTU.TotalMs) * time.Millisecond

			fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%s\n",
				wf.Name,
				wkconclusion["total"],
				wkconclusion["success"],
				wkconclusion["failure"],
				wkconclusion["cancelled"],
				bill,
			)
			allBill += bill
		}

		w.Flush()

	}

	fmt.Println()

	// mins := fmt.Sprintf("%.0f mins", allUsage.Minutes())
	fmt.Printf("Total Bill: %s\n", allBill)
	fmt.Println()
	// if fromFile == "" {
	// 	rateLimit, _, _ = client.RateLimits(ctx)
	// 	// log.Printf("***")

	// 	fmt.Printf("Rate Limit Remain %d\n", rateLimit.Core.Remaining)
	// }
}

func roundUp(d time.Duration) time.Duration {
	var durMin = int(math.Round(d.Minutes()))
	var modSec = 0
	modSec = int(math.Round(d.Seconds())) % 60
	// fmt.Printf("modSec d is : %d\n",
	// 		modSec)

	if modSec > 0 {
		durMin += 1
	}
	t := strconv.Itoa(durMin)
	r, _ := time.ParseDuration(t + "m")

	return r
}

// types.HumanDuration fixes a long string for a value < 1s
func humanDuration(duration time.Duration) string {
	v := strings.ToLower(units.HumanDuration(duration))

	if v == "less than a second" {
		return fmt.Sprintf("%d ms", duration.Milliseconds())
	} else if v == "about a minute" {
		return fmt.Sprintf("%d seconds", int(duration.Seconds()))
	}

	return v
}

func BeginningOfMonth(date time.Time) time.Time {
	return date.AddDate(0, 0, -date.Day()+1)
}

func EndOfMonth(date time.Time) time.Time {
	return date.AddDate(0, 1, -date.Day())
}

func getTeamFromCache(allTeams []*Team, repoName string) Team {
	idx := slices.IndexFunc(allTeams, func(t *Team) bool {
		idx2 := slices.IndexFunc(t.Repos, func(r TeamRepo) bool {
			return r.Name == repoName
		})
		return idx2 > -1

	})
	if idx > -1 {
		return Team{
			Name: allTeams[idx].Name,
			Slug: allTeams[idx].Slug,
		}

	} else {
		// return N/a team
		return Team{
			Name: "N/a",
			Slug: "na",
		}
	}

}
