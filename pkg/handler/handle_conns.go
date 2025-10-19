package handler

import (
	"encoding/json"
	"log"
	"net/http"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
)

type ConnsHandler struct {
	cr *pkgconnreg.ConnRegistry
}

func NewConnsHandler(cr *pkgconnreg.ConnRegistry) *ConnsHandler {
	return &ConnsHandler{cr: cr}
}

func (h *ConnsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.cr.Dump()); err != nil {
		log.Printf("Failed to encode connections: %v", err)
	}
}
