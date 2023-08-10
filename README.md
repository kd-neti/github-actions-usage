## actions-usage

Find your GitHub Actions usage across a given organisation or user account.

[![build](https://github.com/self-actuated/actions-usage/actions/workflows/build.yml/badge.svg)](https://github.com/self-actuated/actions-usage/actions/workflows/build.yml)
![License](https://img.shields.io/github/license/self-actuated/actions-usage)
![Total downloads](https://img.shields.io/github/downloads/self-actuated/actions-usage/total)

![Example console output](https://pbs.twimg.com/media/FrbYxbwWwAMvQZN?format=jpg&name=large)
> Example console output for the [inlets OSS repos](https://github.com/inlets)

Includes total runtime of all workflow runs and workflow jobs, including where the jobs were run within inclusive, free, billed minutes, or on self-hosted runners.

This data is not available within a single endpoint in GitHub's REST or GraphQL APIs, so many different API calls are necessary to build up the usage statistics.

```
repos = ListRepos(organisation || user)
   for each Repo
       ListWorkflowRuns(Repo)
          for each WorkflowRun
             jobs = ListWorkflowJobs(WorkflowRun)
sum(jobs)
```

If your team has hundreds of repositories, or thousands of builds per month, then the tool may exit early due to exceeding the API rate-limit. In this case, we suggest you run with `--days=10` and multiply the value by 3 to get a rough picture of 30-day usage.

## Usage

This tool is primarily designed for use with an organisation, however you can also use it with a regular user account by changing the `--org` flag to `--user`.

Or create a [Classic Token](https://github.com/settings/tokens) with: repo and admin:org and save it to ~/pat.txt. Create a short lived duration for good measure.

We recommend using arkade to install this CLI, however you can also download a binary from the [releases page](https://github.com/self-actuated/actions-usage/releases).

```sh
# sudo is optional for this step
curl -SLs https://get.arkade.dev | sudo sh

arkade get actions-usage
sudo mv $HOME/.arkade/bin/actions-usage /usr/local/bin/
```

## Output

```bash
actions-usage --org openfaas --token-file ~/pat.txt --days 28

Usage report generated by self-actuated/actions-usage.

Total repos: 44
Total private repos: 0
Total public repos: 44

Total workflow runs: 128
Total workflow jobs: 173

Total users: 4
Longest build: 13m50s
Average build time: 2m55s
Total usage: 8h24m21s (504 mins)
```

As a user:

```bash
actions-usage --user alexellis --token-file ~/pat.txt
```


You can use the `-by-repo` flag to get a detailed breakdown of usage by GitHub repository - sorted by number of builds.

```
Longest build: 4h12m40s
Average build time: 2m37s

Repo                                      Builds         Success        Failure        Cancelled      Skipped        Total          Average        Longest
actuated-samples/k3sup-matrix-test        144            140            1              3              0              1h31m33s       38s            2m0s
actuated-samples/specs                    36             25             0              11             0              6m15s          10s            25s
actuated-samples/minty                    24             6              18             0              0              2m32s          6s             10s
actuated-samples/debug-ssh                17             12             1              4              0              9h12m33s       32m30s         4h12m40s
actuated-samples/matrix-build             14             14             0              0              0              31s            2s             4s
actuated-samples/dns                      9              3              0              6              0              40s            4s             18s
actuated-samples/ebpf                     5              2              3              0              0              39s            8s             9s
actuated-samples/cilium-test              4              1              3              0              0              9m16s          2m19s          6m49s
actuated-samples/openfaas-helm            4              2              2              0              0              3m42s          56s            1m54s
actuated-samples/ivan-ssl                 3              3              0              0              0              13s            4s             5s
actuated-samples/kind-tester              1              0              1              0              0              14m56s         14m56s         14m56s
actuated-samples/kernel-builder-linux-6.0 1              1              0              0              0              1m16s          1m16s          1m16s

Total usage: 11h24m6s (684 mins)
```

## Development

All changes must be proposed with an Issue prior to working on them or sending a PR. Commits must have a sign-off message, i.e. `git commit -s`

```bash
git clone https://github.com/actuated/actions-usage
cd actions-usage

go run . --org actuated-samples --token-file ~/pat.txt
```

## Author
ิbankwing

## License

MIT

Contributions are welcome, however an Issue must be raised and approved before submitting a PR.

For typos, raise an Issue and a contributor will fix this. It's easier for everyone that way.

