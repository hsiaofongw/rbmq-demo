package framing

import (
	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
)

type MessagePayload struct {
	Register *pkgconnreg.RegisterPayload `json:"register,omitempty"`
	Echo     *pkgconnreg.EchoPayload     `json:"echo,omitempty"`
}
