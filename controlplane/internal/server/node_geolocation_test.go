package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeResponsePublicIPPrefersHighestConfidenceObservedPublicIP(t *testing.T) {
	stored := "203.0.113.10"
	resp := nodeResponse{
		PublicIP: &stored,
		NetworkObservations: []networkObservationResponse{
			{Kind: "private_ip", Value: "10.0.0.5", Confidence: 100},
			{Kind: "public_ip", Value: "198.51.100.20", Confidence: 70},
			{Kind: "public_ip", Value: "198.51.100.44", Confidence: 95},
		},
	}

	require.Equal(t, "198.51.100.44", nodeResponsePublicIP(resp))
}

func TestNodeResponsePublicIPFallsBackToStoredPublicIP(t *testing.T) {
	stored := "203.0.113.10"
	resp := nodeResponse{
		PublicIP: &stored,
		NetworkObservations: []networkObservationResponse{
			{Kind: "public_ip", Value: "not-an-ip", Confidence: 100},
			{Kind: "private_ip", Value: "10.0.0.5", Confidence: 100},
		},
	}

	require.Equal(t, "203.0.113.10", nodeResponsePublicIP(resp))
}

func TestNodeResponsePublicIPRejectsInvalidEvidence(t *testing.T) {
	stored := "not-an-ip"
	resp := nodeResponse{
		PublicIP: &stored,
		NetworkObservations: []networkObservationResponse{
			{Kind: "public_ip", Value: "invalid", Confidence: 100},
		},
	}

	require.Empty(t, nodeResponsePublicIP(resp))
}
