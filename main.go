package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/go-github/v50/github"
	"golang.org/x/exp/slices"
	"golang.org/x/oauth2"
)

type ResultData struct {
	Date            time.Time `json:"created_at,omitempty"`
	Product         string    `json:"product"`
	SKU             string    `json:"sku"`
	Quantity        int64     `json:"quantity"`
	UnitType        string    `json:"unit_type"`
	Multiplier      int       `json:"multiplier"`
	Owner           string    `json:"owner"`
	RepositorySlug  string    `json:"repository_slug"`
	ActionsWorkflow string    `json:"actions_workflow"`
	CostCenter      string    `json:"cost_center"`
	Project         string    `json:"project"`
	Runs            int       `json:"runs"`
}

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

type JsonData struct {
	Owner         string
	Days          int
	LastIndex     int
	TotalRepo     int
	AllUsageRepos []*UsageRepository
	StartDate     string
	RunDate       string
	AllResultData []*ResultData
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
	Topics       []string            `json:"topics,omitempty"`
}

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

var version string
var build string

func main() {

	var (
		orgName, userName, token, tokensFile, fromFile, output, dateString                  string
		days, tokenIdx, minRateLimit                                                        int
		verbose, getRateLimit, noCache, silent, utc, showVersion, checkToken, includeNoTags bool
		tokens                                                                              []string
	)

	var (
		totalRepo       int
		lastIndex       int
		nextIndex       int
		repos, allRepos []*github.Repository
		allUsageRepos   []*UsageRepository
		allResultData   []*ResultData
	)

	flag.StringVar(&orgName, "org", "", "Organization name")
	flag.StringVar(&userName, "user", "", "User name")
	flag.StringVar(&token, "token", "", "GitHub token")
	flag.StringVar(&tokensFile, "tokens-file", "", "Path to the file containing the GitHub tokens")
	flag.StringVar(&fromFile, "from-file", "", "Path to the json file to process")
	flag.StringVar(&output, "output", "tsv", "output format [tsv, csv, md]")
	flag.StringVar(&dateString, "date", "", "Date to get data")
	flag.IntVar(&minRateLimit, "minlimit", 1, "Min rate limit for token to process")

	flag.BoolVar(&utc, "utc", false, "Use UTC timezone")
	flag.BoolVar(&verbose, "verbose", false, "Verbose Log")
	flag.BoolVar(&silent, "silent", false, "silent Log")
	flag.BoolVar(&noCache, "nocache", false, "No cache")
	flag.BoolVar(&getRateLimit, "rate-limit", false, "Verbose Log")
	flag.BoolVar(&showVersion, "version", false, "Version")
	flag.BoolVar(&checkToken, "check-token", false, "Check token(s)")
	flag.BoolVar(&includeNoTags, "include-notags", false, "Check token(s)")
	// flag.BoolVar(&byRepo, "by-repo", false, "Show breakdown by repository")
	// flag.BoolVar(&byTeam, "by-team", false, "Show breakdown by team")

	// flag.BoolVar(&punchCard, "punch-card", false, "Show punch card with breakdown of builds per day")
	// flag.IntVar(&days, "days", 30, "How many days of data to query from the GitHub API")

	// flag.IntVar(&threadhold, "days", 30, "How many days of data to query from the GitHub API")

	flag.Parse()

	switch {
	case showVersion:
		fmt.Printf("version=%s, build=%s\n", version, build)
		os.Exit(0)
	case checkToken:
		// fmt.Printf("Checking\n")
		if token == "" && tokensFile == "" {
			log.Fatal("token or tokens-file is required")

		} else if tokensFile != "" {
			tokenBytes, err := os.ReadFile(filepath.Clean(tokensFile))
			if err != nil {
				log.Fatal(err)
			}
			tokens = strings.Split(string(tokenBytes), "\n")
			tokens = delete_empty(tokens)

		} else {
			tokens = []string{token}
		}
		todayLocal := time.Now()
		today := todayLocal
		time.Local = time.UTC
		todayUTC := time.Now()

		var layout = "2006-01-02 15:04:05 +0700"

		fmt.Printf("Check Time UTC: %s\n", todayUTC.Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("Check Time LOC: %s\n\n", todayLocal.Format(layout))

		w := tabwriter.NewWriter(os.Stdout, 4, 5, 1, ' ', tabwriter.TabIndent)
		if output == "tsv" {
			fmt.Fprintln(w, "Token\tCode\tExpireDate\tRemainHour\tResult")
			fmt.Fprintln(w, "-----\t------\t-------------------------\t---------------------\t------")

		} else if output == "md" {
			fmt.Fprintln(w, "|Token|Code|Expired Date|Remaining Hours|Result|")
			fmt.Fprintln(w, "|---|---|---|---|---|")

		}

		var overallResult = "✅OK"
		for idx, tk := range tokens {

			// fmt.Printf("Token[%d]: \n", idx+1)
			var statusCode = ""
			var expireString = ""
			var result = "✅OK"
			var diff time.Duration
			requestURL := "https://api.github.com/user"

			req, err := http.NewRequest(http.MethodGet, requestURL, nil)
			if err != nil {
				// fmt.Printf("client: could not create request: %s\n", err)
				statusCode = "Err"
				result = "N/a"
				// os.Exit(1)
			}
			req.Header.Set("Authorization", "Bearer "+tk)

			client := http.Client{
				Timeout: 30 * time.Second,
			}

			res, err := client.Do(req)
			if err != nil {
				// fmt.Printf("client: error making http request: %s\n", err)
				statusCode = "Err"
				result = "N/a"
				// os.Exit(1)
			}
			// fmt.Printf("status code: %d\n", res.StatusCode)
			statusCode = fmt.Sprint(res.StatusCode)
			expireString = res.Header.Get("github-authentication-token-expiration")
			// fmt.Printf("github-authentication-token-expiration: %s\n", res.Header.Get("github-authentication-token-expiration"))

			if expireString != "" {

				if strings.Contains(expireString, "UTC") {
					layout = "2006-01-02 15:04:05 MST"
					today = todayUTC
				} else {
					layout = "2006-01-02 15:04:05 +0700"
					today = todayLocal

				}

				expireTime, errParse := time.Parse(layout, expireString)
				if errParse != nil {
					// panic
					// log.Fatal(errParse)
					result = "Can't parse"
					diff = 0
				} else {
					diff = expireTime.Sub(today)

				}

			}

			if diff <= 0 {
				result = "❌Expired"
				overallResult = "❌Expired"
			} else if diff.Hours() > 0 && diff.Hours() < 72 {
				result = "⚠️Warning"
				if overallResult != "❌Expired" {
					overallResult = "⚠️Warning"

				}

			} else {
				if overallResult != "⚠️Warning" && overallResult != "❌Expired" {
					overallResult = "✅OK"
				}
			}
			if output == "tsv" {
				fmt.Fprintf(w, "%d\t%s\t%s\t%.f\t%s\n", idx+1, statusCode, expireString, diff.Hours(), result)
			} else if output == "md" {
				fmt.Fprintf(w, "|%d|%s|%s|%.f|%s|\n", idx+1, statusCode, expireString, diff.Hours(), result)
			}

		}
		err := w.Flush()
		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		}
		if output == "tsv" {
			fmt.Printf("\n----------------------\nOverall Result: %s\n----------------------\n", overallResult)

		} else if output == "md" {
			fmt.Printf("\n> Overall Result: %s\n", overallResult)
		}
		os.Exit(0)
	case orgName == "" && userName == "" && fromFile == "":
		log.Fatal("Organization name or username or fromFile is required")
	case orgName != "" && userName != "" && fromFile == "":
		log.Fatal("org or username must not be specified at the same time")
	case token != "" && tokensFile != "":
		log.Fatal("token or tokens-file must not be specified at the same time")
	case token == "" && tokensFile == "":
		log.Fatal("token or tokens-file is required")
	case verbose && silent:
		log.Fatal("verbose or silent must not be specified at the same time")
	case output != "" && output != "tsv" && output != "csv" && output != "file":
		log.Fatal("Only tsv, csv or file for output")

	}

	if fromFile == "" && tokensFile != "" {
		tokenBytes, err := os.ReadFile(filepath.Clean(tokensFile))
		if err != nil {
			log.Fatal(err)
		}
		tokens = strings.Split(string(tokenBytes), "\n")
		tokens = delete_empty(tokens)
		if len(tokens) > 0 {
			tokenIdx = 0
			token = tokens[tokenIdx]
		} else {
			token = ""
		}
		// token = strings.TrimSpace(string(tokenBytes))
	}

	auth := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))

	var res *github.Response
	var err error
	var rateLimit *github.RateLimits

	if utc {
		time.Local = time.UTC
	}

	client := github.NewClient(auth)
	today := time.Now()
	if dateString != "" {
		today, err = time.Parse("2006-01-02", dateString)
		if err != nil {
			// panic
			log.Fatal(err)
		}
	}

	// days = today.Day()
	days = 1
	// created := today.AddDate(0, 0, -days+1)
	created := today
	format := "2006-01-02"
	createdQuery := today.Format(format) + ".." + today.Format(format)

	ctx := context.Background()
	page := 0

	totalRepo = 0
	lastIndex = 0
	nextIndex = 0

	var owner string
	if orgName != "" {
		owner = orgName
	}
	if userName != "" {
		owner = userName
	}

	// Fetching online
	if fromFile == "" {
		// Try to Read data from cache first
		var cacheFile = "cache/" + today.Format(format) + ".json"
		var data JsonData
		if _, err := os.Stat(cacheFile); err == nil && !noCache {
			jsonBytes, err := os.ReadFile(filepath.Clean(cacheFile))
			if err != nil {
				log.Fatal(err)
			}

			err = json.Unmarshal(jsonBytes, &data)
			if err != nil {
				// panic
				log.Fatal(err)
			}
			owner = data.Owner
			days = data.Days
			allUsageRepos = data.AllUsageRepos
			allResultData = data.AllResultData
			totalRepo = data.TotalRepo
			lastIndex = data.LastIndex
			nextIndex = data.LastIndex + 1

		}

		if totalRepo == 0 || noCache {
			allUsageRepos = nil
			allResultData = nil
			totalRepo = 0
			lastIndex = -1
			nextIndex = 0

			// Fetching from github api
			if verbose {
				if noCache {
					log.Printf("Force no cache\n")
				}

				log.Printf("Fetching last %d days of data (created=%s)\n", days, created.Format("2006-01-02"))
			}

			checkRateLimit(minRateLimit, tokens, &tokenIdx, &client, ctx, verbose)

			for {
				if verbose {
					log.Printf("Fetching repos %s page %d", orgName, page)
				}
				if orgName != "" {
					opts := &github.RepositoryListByOrgOptions{ListOptions: github.ListOptions{Page: page, PerPage: 100}, Type: "all"}
					repos, res, err = client.Repositories.ListByOrg(ctx, orgName, opts)

				} else if userName != "" {
					opts := &github.RepositoryListOptions{ListOptions: github.ListOptions{Page: page, PerPage: 100}, Type: "all"}
					repos, res, err = client.Repositories.List(ctx, userName, opts)

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
					err = json.Unmarshal([]byte(record), &usageRepo)
					if err != nil {
						fmt.Printf("Error: %s", err.Error())
					}
					allUsageRepos = append(allUsageRepos, &usageRepo)
				}

				allRepos = append(allRepos, repos...)
				if verbose {
					log.Printf("Status: %d Page %d, next page: %d", res.StatusCode, page, res.NextPage)
				}
				if len(allRepos) == 0 {
					break
				}
				if res.NextPage == 0 {
					break
				}

				// break
				page = res.NextPage
			}

			totalRepo = len(allUsageRepos)
		}

		if verbose {
			log.Printf("Total repos: %d", totalRepo)
			log.Printf("Last run repo index: %d", lastIndex)
			if lastIndex < totalRepo-1 {
				log.Printf("Continue at repo index: %d", nextIndex)

			} else if lastIndex > -1 && lastIndex == totalRepo-1 {
				log.Printf("Nothing to do")
				os.Exit(0)
			}

		}

		for i := nextIndex; i < len(allUsageRepos); i++ {
			repo := allUsageRepos[i]
			CostCenter := "N/a"
			Project := "N/a"

			if len(repo.Topics) > 0 {
				for _, topic := range repo.Topics {
					c := strings.Index(topic, "costcenter-")
					if c == 0 {
						CostCenter = topic
					}
					p := strings.Index(topic, "project-")
					if p == 0 {
						Project = topic
					}
				}

			}

			if !includeNoTags && CostCenter == "N/a" && Project == "N/a" {
				if verbose {
					log.Printf("Get[%d]: %s --- SKIP NO TOPICS", i, repo.FullName)
				}

			} else {

				if verbose {
					log.Printf("Get[%d]: %s", i, repo.FullName)
				} else if !silent {
					fmt.Printf("\033[2K\rRepos: %d/%d", i+1, len(allUsageRepos))
				}

				page := 0
				for {
					if verbose {
						log.Printf("Listing workflows for: %s page: %d", repo.FullName, page)
					} else if !silent {
						fmt.Printf("\033[2K\rRepos: %d/%d - Listing workflows: %d", i+1, len(allUsageRepos), page)
					}
					checkRateLimit(minRateLimit, tokens, &tokenIdx, &client, ctx, verbose)

					var wkflow *github.Workflows
					opts := &github.ListOptions{Page: page, PerPage: 100}
					if orgName != "" {
						wkflow, res, err = client.Actions.ListWorkflows(ctx, orgName, repo.Name, opts)

					}

					if userName != "" {
						realOwner := userName
						if userName != repo.Owner.Login {
							realOwner = repo.Owner.Login
						}
						wkflow, res, err = client.Actions.ListWorkflows(ctx, realOwner, repo.Name, opts)

					}

					if err != nil {
						log.Print(err)
						useNextToken(tokens, &tokenIdx, &client, ctx, verbose)
						// i--
						continue
					}

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

					// break
					page = res.NextPage
				}

				if verbose {
					log.Printf("Found %d workflows for %s", len(allUsageRepos[i].Workflows), repo.FullName)
				}

				workflowRuns := []*github.WorkflowRun{}

				page = 0
				for {
					checkRateLimit(minRateLimit, tokens, &tokenIdx, &client, ctx, verbose)

					opts := &github.ListWorkflowRunsOptions{Created: createdQuery, ListOptions: github.ListOptions{Page: page, PerPage: 100}}

					var runs *github.WorkflowRuns
					if verbose {
						log.Printf("Listing workflow runs for: %s page %d", repo.FullName, page)
					} else if !silent {
						fmt.Printf("\033[2K\rRepos: %d/%d - Listing workflow runs: %d", i+1, len(allRepos), page)
					}

					if orgName != "" {
						runs, res, err = client.Actions.ListRepositoryWorkflowRuns(ctx, orgName, repo.Name, opts)

					}
					if userName != "" {
						realOwner := userName
						// if user is a member of repository
						if userName != repo.Owner.Login {
							realOwner = repo.Owner.Login
						}
						runs, res, err = client.Actions.ListRepositoryWorkflowRuns(ctx, realOwner, repo.Name, opts)

					}

					if err != nil {
						log.Print(err)
						useNextToken(tokens, &tokenIdx, &client, ctx, verbose)

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
				if verbose {
					log.Printf("Found %d workflow runs for %s/%s", len(workflowRuns), owner, repo.Name)
				}

				record, _ := json.Marshal(workflowRuns)
				var usageWorkflows []*UsageWorkflowRun
				err = json.Unmarshal([]byte(record), &usageWorkflows)
				if err != nil {
					fmt.Printf("Error: %s", err.Error())
				}
				allUsageRepos[i].WorkflowRuns = usageWorkflows

				if dateString != "" {
					// Case provide dateString
					for wi, workflow := range allUsageRepos[i].Workflows {
						idx := slices.IndexFunc(allUsageRepos[i].WorkflowRuns, func(wr *UsageWorkflowRun) bool { return wr.WorkflowID == workflow.ID })
						if !verbose {
							fmt.Printf("\033[2K\rRepos: %d/%d - WorkflowUsage: %d/%d", i+1, len(allRepos), wi, len(allUsageRepos[i].Workflows))

						} else if verbose {
							if idx == -1 {
								log.Printf("SKIP Workflow: %s", workflow.Path)
							} else if idx > -1 {
								log.Printf("Get WorkflowUsage for workflow: %s", workflow.Path)
							}

						}

						if idx > -1 {
							runs := 0
							qty_ubuntu := 0
							qty_windows := 0
							qty_macos := 0
							for _, run := range allUsageRepos[i].WorkflowRuns {
								if run.WorkflowID == workflow.ID {
									runs += 1

									if verbose {
										log.Printf("Get WorkflowRunUsage for RunID: %d", run.ID)
									}
									checkRateLimit(minRateLimit, tokens, &tokenIdx, &client, ctx, verbose)

									var workflowRunUsage *github.WorkflowRunUsage

									workflowRunUsage, res, _ = client.Actions.GetWorkflowRunUsageByID(ctx, orgName, repo.Name, run.ID)

									billable := workflowRunUsage.GetBillable()
									// fmt.Println(workflowRunUsage.GetBillable())
									if val, ok := (*billable)["UBUNTU"]; ok {
										qty_ubuntu += int(val.GetTotalMS() / 60000)

									}
									if val, ok := (*billable)["WINDOWS"]; ok {
										qty_windows += int(val.GetTotalMS() / 60000)

									}
									if val, ok := (*billable)["MACOS"]; ok {
										qty_macos += int(val.GetTotalMS() / 60000)

									}

								}
							}

							var resultData ResultData

							resultData.Date = today
							resultData.Owner = owner
							resultData.Product = "Action"
							resultData.UnitType = "minute"
							resultData.CostCenter = CostCenter
							resultData.Project = Project
							resultData.RepositorySlug = allUsageRepos[i].Name

							file_name := strings.TrimRight(workflow.Path, "/")
							file_name = strings.Split(file_name, "/")[len(strings.Split(file_name, "/"))-1]

							resultData.ActionsWorkflow = file_name
							resultData.Runs = runs

							if qty_ubuntu > 0 {
								resultData.Multiplier = 1
								resultData.SKU = "Compute - UBUNTU"
								resultData.Quantity = int64(qty_ubuntu)
								allResultData = append(allResultData, &resultData)

							}
							if qty_windows > 0 {
								resultData.Multiplier = 2
								resultData.SKU = "Compute - WINDOWS"
								resultData.Quantity = int64(qty_windows)
								allResultData = append(allResultData, &resultData)

							}
							if qty_macos > 0 {
								resultData.Multiplier = 10
								resultData.SKU = "Compute - MACOS"
								resultData.Quantity = int64(qty_macos)
								allResultData = append(allResultData, &resultData)

							}

						}
					}

				} else {
					// Case datestring not provide use current date
					for wi, workflow := range allUsageRepos[i].Workflows {
						if !verbose {
							fmt.Printf("\033[2K\rRepos: %d/%d - WorkflowUsage: %d/%d", i+1, len(allRepos), wi, len(allUsageRepos[i].Workflows))

						}
						idx := slices.IndexFunc(allUsageRepos[i].WorkflowRuns, func(wr *UsageWorkflowRun) bool { return wr.WorkflowID == workflow.ID })
						if idx > -1 {
							if verbose {
								log.Printf("Get WorkflowUsage for: %s", workflow.Path)
							}

							checkRateLimit(minRateLimit, tokens, &tokenIdx, &client, ctx, verbose)

							runs := 0
							for _, run := range allUsageRepos[i].WorkflowRuns {
								if run.WorkflowID == workflow.ID {
									runs += 1
								}
							}

							var workflowUsage *github.WorkflowUsage

							workflowUsage, res, err = client.Actions.GetWorkflowUsageByID(ctx, orgName, repo.Name, workflow.ID)
							// log.Printf("***")

							var resultData ResultData

							resultData.Date = today
							resultData.Owner = owner
							resultData.Product = "Action"
							resultData.UnitType = "minute"
							resultData.CostCenter = CostCenter
							resultData.Project = Project
							resultData.RepositorySlug = allUsageRepos[i].Name

							file_name := strings.TrimRight(workflow.Path, "/")
							file_name = strings.Split(file_name, "/")[len(strings.Split(file_name, "/"))-1]

							resultData.ActionsWorkflow = file_name
							resultData.Runs = runs

							if err != nil {
								log.Print(err)
							} else {
								billable := workflowUsage.GetBillable()
								// fmt.Println(workflowRunUsage.GetBillable())
								if val, ok := (*billable)["UBUNTU"]; ok {
									resultData.Multiplier = 1
									resultData.SKU = "Compute - UBUNTU"
									resultData.Quantity = val.GetTotalMS() / 60000
									if resultData.Quantity > 0 {
										allResultData = append(allResultData, &resultData)
									}

								}
								if val, ok := (*billable)["WINDOWS"]; ok {
									resultData.Multiplier = 2
									resultData.SKU = "Compute - WINDOWS"
									resultData.Quantity = val.GetTotalMS() / 60000
									if resultData.Quantity > 0 {
										allResultData = append(allResultData, &resultData)
									}

								}
								if val, ok := (*billable)["MACOS"]; ok {
									resultData.Multiplier = 10
									resultData.SKU = "Compute - MACOS"
									resultData.Quantity = val.GetTotalMS() / 60000
									if resultData.Quantity > 0 {
										allResultData = append(allResultData, &resultData)
									}

								}
							}

						}
					}
				}
			}

			// Write to cache when get all data for each repo
			if verbose {
				log.Printf("Done[%d] Write cache runIndex: %d\n", i, i)
			}
			rundate := time.Now()

			data := JsonData{
				// TotalRuns:    totalRuns,
				// TotalJobs:    totalJobs,
				LastIndex: i,
				TotalRepo: len(allUsageRepos),
				// AllRepos:        allRepos,
				AllUsageRepos: allUsageRepos,
				// Actors:        actors,
				// Conclusion:    conclusion,
				Owner:     owner,
				Days:      days,
				StartDate: created.Format(format),
				RunDate:   rundate.Format(format),
				// AllUsage:      allUsage,
				AllResultData: allResultData,
			}

			jsonData, err := json.MarshalIndent(data, "", "  ")

			if err != nil {
				fmt.Printf("Error: %s", err.Error())
			} else {
				// fmt.Println(string(jsonData))
				err = os.MkdirAll("cache", os.ModePerm)
				if err != nil {
					fmt.Printf("Error: %s", err.Error())
				}
				err = os.WriteFile(cacheFile, jsonData, 0644)
				if err != nil {
					fmt.Printf("Error: %s", err.Error())
				}
			}

		}

		rateLimit, _, err = client.RateLimits(ctx)
		if err != nil {
			log.Fatal(err)
		}
		if verbose {
			log.Printf("Rate Limit Remain: %d\n", rateLimit.Core.Remaining)
		}

	} else if fromFile != "" {

		// Read data from file
		jsonBytes, err := os.ReadFile(filepath.Clean(fromFile))
		if err != nil {
			log.Fatal(err)
		}
		var data JsonData

		err = json.Unmarshal(jsonBytes, &data)
		if err != nil {
			// panic
			log.Fatal(err)
		}
		owner = data.Owner
		days = data.Days

		allResultData = data.AllResultData

		if verbose {
			log.Printf("Reading data from file: %s \n", fromFile)
			fmt.Printf("Fetching last %d days of data (created>=%s - %s)\n", days, data.StartDate, data.RunDate)
		}

	}

	// OUTPUT
	if output == "csv" {
		fmt.Printf("\033[2K\r")
		sort.Slice(allResultData, func(i, j int) bool {
			return allResultData[i].Quantity > allResultData[j].Quantity

		})

		w := tabwriter.NewWriter(os.Stdout, 4, 5, 1, ' ', tabwriter.TabIndent)
		// fmt.Fprintln(w, "Workflow\tRuns\tSuccess\tFailure\tCancelled\tTotal\tBill\tAverage\tLongest")
		fmt.Fprintln(w, "Date,Product,SKU,Quantity,Unit Type,Multiplier,Owner,Repository Slug,Cost Center,Project,Runs,Actions Workflow")
		for _, r := range allResultData {
			if r.Quantity > 0 {
				fmt.Fprintf(w, "%s,%s,%s,%d,%s,%d,%s,%s,%s,%s,%d,%s\n",
					r.Date.Format(format),
					r.Product,
					r.SKU,
					r.Quantity,
					r.UnitType,
					r.Multiplier,
					r.Owner,
					r.RepositorySlug,
					r.CostCenter,
					r.Project,
					r.Runs,
					r.ActionsWorkflow,
				)
			}

		}
		err = w.Flush()
		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		}

	} else if output == "tsv" {
		fmt.Printf("\033[2K\r")
		if dateString != "" {
			fmt.Printf("Type: Accumulate\n")

		}
		fmt.Printf("Owner: %s\n\n", owner)

		sort.Slice(allResultData, func(i, j int) bool {
			return allResultData[i].Quantity > allResultData[j].Quantity
		})

		w := tabwriter.NewWriter(os.Stdout, 4, 5, 1, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "Date\tSKU\tQty\tUnit\tMul\tRepository Slug\tCost Center\tProject\tRuns\tActions Workflow")
		fmt.Fprintln(w, "----\t---\t---\t----\t---\t---------------\t------------\t-------\t----\t---------------")
		for _, r := range allResultData {
			if r.Quantity > 0 {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\t%s\t%s\t%s\t%d\t%s\n",
					r.Date.Format(format),
					// r.Product,
					r.SKU[10:],
					r.Quantity,
					r.UnitType[:3],
					r.Multiplier,
					// r.Owner,
					r.RepositorySlug,
					r.CostCenter,
					r.Project,
					r.Runs,
					r.ActionsWorkflow,
				)
			}
		}
		err = w.Flush()
		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		}

	} else if output == "file" {
		fmt.Printf("\033[2K\r")
		sort.Slice(allResultData, func(i, j int) bool {
			return allResultData[i].Quantity > allResultData[j].Quantity

		})

		reportFileName := "report-" + today.Format(format) + ".csv"
		if dateString != "" {
			reportFileName = "report-" + today.Format(format) + "-nonaccum.csv"
		}
		f, err := os.Create(filepath.Clean(reportFileName))
		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		}
		defer f.Close()

		_, err = fmt.Fprintln(f, "Date,Product,SKU,Quantity,Unit Type,Multiplier,Owner,Repository Slug,Cost Center,Project,Runs,Actions Workflow")
		if err != nil {
			fmt.Printf("Error: %s", err.Error())
		}
		for _, r := range allResultData {
			if r.Quantity > 0 {
				fmt.Fprintf(f, "%s,%s,%s,%d,%s,%d,%s,%s,%s,%s,%d,%s\n",
					r.Date.Format(format),
					r.Product,
					r.SKU,
					r.Quantity,
					r.UnitType,
					r.Multiplier,
					r.Owner,
					r.RepositorySlug,
					r.CostCenter,
					r.Project,
					r.Runs,
					r.ActionsWorkflow,
				)
			}
		}

		if verbose {
			log.Printf("Write report to file: %s\n", reportFileName)
		}

	}

}

func BeginningOfMonth(date time.Time) time.Time {
	return date.AddDate(0, 0, -date.Day()+1)
}

func EndOfMonth(date time.Time) time.Time {
	return date.AddDate(0, 1, -date.Day())
}

func delete_empty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}
func useNextToken(tokens []string, tokenIdx *int, client **github.Client, ctx context.Context, verbose bool) {
	if *tokenIdx < len(tokens)-1 {
		if verbose {
			log.Printf("Use Next TOKEN ***\n")
			// log.Printf("%s\n", tokens[*tokenIdx])

		}
		*tokenIdx += 1
		token := tokens[*tokenIdx]
		auth := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		))
		*client = github.NewClient(auth)
	} else {
		if verbose {
			log.Printf("No token left wait next hour\n")
		}
		os.Exit(0)
	}
}

func checkRateLimit(minRateLimit int, tokens []string, tokenIdx *int, client **github.Client, ctx context.Context, verbose bool) {
	token := tokens[*tokenIdx]
	auth := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
	newclient := github.NewClient(auth)

	chkrateLimit, _, err := newclient.RateLimits(ctx)
	for {
		if err != nil {
			if *tokenIdx >= len(tokens)-1 {
				log.Fatal(err)

			} else {
				if verbose {
					log.Print(err)
				}
				useNextToken(tokens, tokenIdx, client, ctx, verbose)
				newtokenIdx := *tokenIdx
				token := tokens[newtokenIdx]
				auth := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
					&oauth2.Token{AccessToken: token},
				))
				newclient = github.NewClient(auth)
				chkrateLimit, _, err = newclient.RateLimits(ctx)

			}
		} else {
			if verbose {
				log.Printf("Rate Limit Remain: %d\n", chkrateLimit.Core.Remaining)
			}
			break
		}
	}
	if chkrateLimit.Core.Remaining < minRateLimit {
		if verbose {
			log.Printf("Token has rate limit less than %d\n", minRateLimit)
		}
		useNextToken(tokens, tokenIdx, client, ctx, verbose)

	}
}
