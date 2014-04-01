package queue

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/drone/drone/pkg/build/script"
	"github.com/drone/drone/pkg/database"
	. "github.com/drone/drone/pkg/model"
	"github.com/drone/go-github/github"
	"github.com/drone/go-bitbucket/bitbucket"
)

// A Queue dispatches tasks to workers.
type Queue struct {
	mu sync.Mutex
	pending map[string]chan int64
	tasks chan<- *BuildTask
}

// BuildTasks represents a build that is pending
// execution.
type BuildTask struct {
	Repo   *Repo
	Commit *Commit
	Build  *Build

	// Build instructions from the .drone.yml
	// file, unmarshalled.
	Script *script.Build

	finished chan int64
}

// Start N workers with the given build runner.
func Start(workers int, runner BuildRunner) *Queue {
	tasks := make(chan *BuildTask)

	queue := &Queue{
		pending: make(map[string]chan int64),
		tasks: tasks,
	}

	for i := 0; i < workers; i++ {
		worker := worker{
			runner: runner,
		}

		go worker.work(tasks)
	}

	return queue
}

func (q *Queue) Process(repoSlug string, commit *Commit) error {
	repo, err := database.GetRepoSlug(repoSlug)
	if err != nil {
		return err
	}

	commit.RepoID = repo.ID

	user, err := database.GetUser(repo.UserID)
	if err != nil {
		return err
	}

	taskID := fmt.Sprintf("%s@%s/%s/%s/commit/%s", repo.UserID, repo.Host, repo.Owner, repo.Name, commit.Hash)
	notifications, claimed := q.claimTask(taskID)
	if !claimed {
		var buildID int64 = -1
		for notification := range notifications {
			fmt.Printf("Slave got notification: %v", notification)
			buildID = notification
		}
		if buildID == -1 {
			return errors.New("Build failed")
		}
		return nil
	}
	defer q.releaseTask(taskID)

	settings := database.SettingsMust()

	var data []byte
	if repo.Host == "bitbucket.org" {
		client := bitbucket.New(
			settings.BitbucketKey,
			settings.BitbucketSecret,
			user.BitbucketToken,
			user.BitbucketSecret,
		)

		raw, err := client.Sources.Find(repo.Owner, repo.Name, commit.Hash, ".drone.yml")
		if err != nil {
			msg := "No .drone.yml was found in this repository.  You need to add one.\n"
			if err := saveFailedBuild(commit, msg); err != nil {
				return err
			}
			return err
		}

		data = []byte(raw.Data)
	} else {
		client := github.New(user.GithubToken)
		client.ApiUrl = settings.GitHubApiUrl

		content, err := client.Contents.FindRef(repo.Owner, repo.Name, ".drone.yml", commit.Hash)
		if err != nil {
			msg := "No .drone.yml was found in this repository.  You need to add one.\n"
			if err := saveFailedBuild(commit, msg); err != nil {
				return err
			}
			return err
		}

		data, err = content.DecodeContent()
		if err != nil {
			msg := "Could not decode the yaml from GitHub.  Check that your .drone.yml is a valid yaml file.\n"
			if err := saveFailedBuild(commit, msg); err != nil {
				return err
			}
			return err
		}
	}

	// parse the build script
	buildscript, err := script.ParseBuild(data, repo.Params)
	if err != nil {
		msg := "Could not parse your .drone.yml file.  It needs to be a valid drone yaml file.\n\n" + err.Error() + "\n"
		if err := saveFailedBuild(commit, msg); err != nil {
			return err
		}
		return err
	}

	// save the commit to the database
	if err := database.SaveCommit(commit); err != nil {
		return err
	}

	// save the build to the database
	build := &Build{}
	build.Slug = "1" // TODO
	build.CommitID = commit.ID
	build.Created = time.Now().UTC()
	build.Status = "Pending"
	if err := database.SaveBuild(build); err != nil {
		return err
	}

	// notify websocket that a new build is pending
	//realtime.CommitPending(repo.UserID, repo.TeamID, repo.ID, commit.ID, repo.Private)
	//realtime.BuildPending(repo.UserID, repo.TeamID, repo.ID, commit.ID, build.ID, repo.Private)

	var buildID int64 = -1
	var finished chan int64
	q.tasks <- &BuildTask{Repo: repo, Commit: commit, Build: build, Script: buildscript, finished: finished}
	for result := range finished {
		fmt.Printf("Build finished: %v", result)
		buildID = result
	}

	if buildID == -1 {
		return errors.New("Build failed")
	}

	notifications <- buildID
	return nil
}

func (q *Queue) claimTask(taskID string) (chan int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	claimed := false
	notifications, ok := q.pending[taskID]
	if !ok {
		notifications = make(chan int64)
		q.pending[taskID] = notifications
		claimed = true
	}

	return notifications, claimed
}

func (q *Queue) releaseTask(taskID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	notifications := q.pending[taskID]
	close(notifications)

	delete(q.pending, taskID)
}

// Helper method for saving a failed build or commit in the case where it never starts to build.
// This can happen if the yaml is bad or doesn't exist.
func saveFailedBuild(commit *Commit, msg string) error {

	// Set the commit to failed
	commit.Status = "Failure"
	commit.Created = time.Now().UTC()
	commit.Finished = commit.Created
	commit.Duration = 0
	if err := database.SaveCommit(commit); err != nil {
		return err
	}

	// save the build to the database
	build := &Build{}
	build.Slug = "1" // TODO: This should not be hardcoded
	build.CommitID = commit.ID
	build.Created = time.Now().UTC()
	build.Finished = build.Created
	commit.Duration = 0
	build.Status = "Failure"
	build.Stdout = msg
	if err := database.SaveBuild(build); err != nil {
		return err
	}

	// TODO: Should the status be Error instead of Failure?

	// TODO: Do we need to update the branch table too?

	return nil
}

