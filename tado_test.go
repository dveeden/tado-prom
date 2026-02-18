package main

import "testing"

func TestTadoRateLimit(t *testing.T) {
	cases := []struct {
		header string
		limit  TadoRateLimit
	}{
		{
			"\"perday\";r=123",
			TadoRateLimit{
				LimitType: "perday",
				Remaining: 123,
				Refill:    0,
			},
		},
		{
			"\"perday\";r=0,t=123",
			TadoRateLimit{
				LimitType: "perday",
				Remaining: 0,
				Refill:    123,
			},
		},
	}

	for _, c := range cases {
		res := rateLimiteFromHeader(c.header)
		if res.LimitType != c.limit.LimitType {
			t.Errorf("Type expected %s but got %s", c.limit.LimitType, res.LimitType)
		}
	}
}
