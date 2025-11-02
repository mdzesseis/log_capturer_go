package sinks

import (
	"crypto/sha256"
	"crypto/sha512"

	"github.com/xdg-go/scram"
)

var (
	// SHA256 é o gerador de hash SHA256
	SHA256 scram.HashGeneratorFcn = sha256.New

	// SHA512 é o gerador de hash SHA512
	SHA512 scram.HashGeneratorFcn = sha512.New
)

// XDGSCRAMClient implementa sarama.SCRAMClient usando xdg-go/scram
type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

// Begin inicia uma nova conversa SCRAM
func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

// Step processa um step da autenticação SCRAM
func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

// Done verifica se a autenticação SCRAM está completa
func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
