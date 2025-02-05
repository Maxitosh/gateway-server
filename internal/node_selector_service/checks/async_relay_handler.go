package checks

import (
	"pokt_gateway_server/internal/node_selector_service/models"
	"pokt_gateway_server/pkg/pokt/pokt_v0"
	relayer_models "pokt_gateway_server/pkg/pokt/pokt_v0/models"
	"sync"
)

type nodeRelayResponse struct {
	Node  *models.QosNode
	Relay *relayer_models.SendRelayResponse
	Error error
}

func SendRelaysAsync(relayer pokt_v0.PocketRelayer, nodes []*models.QosNode, payload string, method string) chan *nodeRelayResponse {
	// Define a channel to receive relay responses
	relayResponses := make(chan *nodeRelayResponse, len(nodes))
	var wg sync.WaitGroup

	// Define a function to handle sending relay requests concurrently
	sendRelayAsync := func(node *models.QosNode) {
		defer wg.Done()
		relay, err := relayer.SendRelay(&relayer_models.SendRelayRequest{
			Signer:             node.GetAppStakeSigner(),
			Payload:            &relayer_models.Payload{Data: payload, Method: method},
			Chain:              node.GetChain(),
			SelectedNodePubKey: node.GetPublicKey(),
			Session:            node.MorseSession,
		})
		relayResponses <- &nodeRelayResponse{
			Node:  node,
			Relay: relay,
			Error: err,
		}
	}

	// Start a goroutine for each node to send relay requests concurrently
	for _, node := range nodes {
		wg.Add(1)
		go sendRelayAsync(node)
	}

	wg.Wait()
	close(relayResponses)

	return relayResponses
}
