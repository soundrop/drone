package handler

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/drone/drone/pkg/model"
	"github.com/drone/drone/pkg/queue"
)

type ArtifactsHandler struct {
	queue *queue.Queue
}

func NewArtifactsHandler(queue *queue.Queue) *ArtifactsHandler {
	return &ArtifactsHandler{
		queue: queue,
	}
}

func (h *ArtifactsHandler) GetArtifact(w http.ResponseWriter, r *http.Request) error {
	hostParam := r.FormValue(":host")
	userParam := r.FormValue(":owner")
	nameParam := r.FormValue(":name")
	commitParam := r.FormValue(":commit")
	repoSlug := fmt.Sprintf("%s/%s/%s", hostParam, userParam, nameParam)

	commit := &Commit{}
	commit.Branch = "master" // TODO
	commit.Hash = commitParam
	commit.Status = "Pending"
	commit.Created = time.Now().UTC() // TODO
	commit.Message = "FIXME" // TODO
	commit.Timestamp = "FIXME" // TODO
	commit.SetAuthor("FIXME") // TODO

	err := h.queue.Process(repoSlug, commit)

	return RenderText(w, fmt.Sprintf ("Err is: %v", err), http.StatusOK)
}
