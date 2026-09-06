package main

import "testing"

func TestPercentageFailureIsEvenlyDistributed(t *testing.T) {
	for _, test := range []struct {
		percentage int
		requests   int
		wantFails  int
	}{
		{percentage: 0, requests: 100, wantFails: 0},
		{percentage: 80, requests: 10, wantFails: 8},
		{percentage: 80, requests: 100, wantFails: 80},
		{percentage: 100, requests: 100, wantFails: 100},
	} {
		fails := 0
		for request := 0; request < test.requests; request++ {
			if percentageFailure(uint64(request), test.percentage) {
				fails++
			}
		}
		if fails != test.wantFails {
			t.Fatalf("percentage=%d requests=%d failures=%d, want %d", test.percentage, test.requests, fails, test.wantFails)
		}
	}
}
