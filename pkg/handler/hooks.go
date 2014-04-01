package handler

import (
	"net/http"
	"strconv"
	"time"

	. "github.com/drone/drone/pkg/model"
	"github.com/drone/drone/pkg/queue"
	"github.com/drone/go-github/github"
	"github.com/drone/go-bitbucket/bitbucket"
)

type HookHandler struct {
	queue *queue.Queue
}

func NewHookHandler(queue *queue.Queue) *HookHandler {
	return &HookHandler{
		queue: queue,
	}
}

// Processes a generic POST-RECEIVE GitHub hook and
// attempts to trigger a build.
func (h *HookHandler) HookGithub(w http.ResponseWriter, r *http.Request) error {
	// handle github ping
	if r.Header.Get("X-Github-Event") == "ping" {
		return RenderText(w, http.StatusText(http.StatusOK), http.StatusOK)
	}

	// if this is a pull request route to a different handler
	if r.Header.Get("X-Github-Event") == "pull_request" {
		h.PullRequestHookGithub(w, r)
		return nil
	}

	payload := r.FormValue("payload")

	hook, err := github.ParseHook([]byte(payload))
	if err != nil {
		println("could not parse hook")
		return RenderText(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	}

	// make sure this is being triggered because of a commit
	// and not something like a tag deletion or whatever
	if hook.IsTag() || hook.IsGithubPages() ||
		hook.IsHead() == false || hook.IsDeleted() {
		return RenderText(w, http.StatusText(http.StatusOK), http.StatusOK)
	}

	repoId := r.FormValue("id")

	commit := &Commit{}
	commit.Branch = hook.Branch()
	commit.Hash = hook.Head.Id
	commit.Status = "Pending"
	commit.Created = time.Now().UTC()

	// extract the author and message from the commit
	// this is kind of experimental, since I don't know
	// what I'm doing here.
	if hook.Head != nil && hook.Head.Author != nil {
		commit.Message = hook.Head.Message
		commit.Timestamp = hook.Head.Timestamp
		commit.SetAuthor(hook.Head.Author.Email)
	} else if hook.Commits != nil && len(hook.Commits) > 0 && hook.Commits[0].Author != nil {
		commit.Message = hook.Commits[0].Message
		commit.Timestamp = hook.Commits[0].Timestamp
		commit.SetAuthor(hook.Commits[0].Author.Email)
	}

	if err := h.queue.Process(repoId, commit); err != nil {
		return RenderText(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}

	return RenderText(w, http.StatusText(http.StatusOK), http.StatusOK)
}

func (h *HookHandler) PullRequestHookGithub(w http.ResponseWriter, r *http.Request) {
	payload := r.FormValue("payload")

	hook, err := github.ParsePullRequestHook([]byte(payload))
	if err != nil {
		RenderText(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if hook.Action != "opened" && hook.Action != "synchronize" {
		RenderText(w, http.StatusText(http.StatusOK), http.StatusOK)
		return
	}

	repoId := r.FormValue("id")

	commit := &Commit{}
	commit.Branch = hook.PullRequest.Head.Ref
	commit.Hash = hook.PullRequest.Head.Sha
	commit.Status = "Pending"
	commit.Created = time.Now().UTC()
	commit.Gravatar = hook.PullRequest.User.GravatarId
	commit.Author = hook.PullRequest.User.Login
	commit.PullRequest = strconv.Itoa(hook.Number)
	commit.Message = hook.PullRequest.Title

	if err := h.queue.Process(repoId, commit); err != nil {
		RenderText(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}

	RenderText(w, http.StatusText(http.StatusOK), http.StatusOK)
}

// Processes a generic POST-RECEIVE Bitbucket hook and
// attempts to trigger a build.
func (h *HookHandler) HookBitbucket(w http.ResponseWriter, r *http.Request) error {
	payload := r.FormValue("payload")

	hook, err := bitbucket.ParseHook([]byte(payload))
	if err != nil {
		return err
	}

	repoId := r.FormValue("id")

	commit := &Commit{}
	commit.Branch = hook.Commits[len(hook.Commits)-1].Branch
	commit.Hash = hook.Commits[len(hook.Commits)-1].Hash
	commit.Status = "Pending"
	commit.Created = time.Now().UTC()
	commit.Message = hook.Commits[len(hook.Commits)-1].Message
	commit.Timestamp = time.Now().UTC().String()
	commit.SetAuthor(hook.Commits[len(hook.Commits)-1].Author)

	if err := h.queue.Process(repoId, commit); err != nil {
		return RenderText(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}

	return RenderText(w, http.StatusText(http.StatusOK), http.StatusOK)
}
