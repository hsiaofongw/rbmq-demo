package handler

import (
	"net/http"
)

type PingTaskHandler struct{}

func NewPingTaskHandler() *PingTaskHandler {
	return &PingTaskHandler{}
}

type PingTaskApplicationForm struct {
	Targets []string `json:"targets"`
}

func (handler *PingTaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

}
