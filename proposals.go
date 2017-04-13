// Copyright (c) 2017 Dave Collins
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"

	"github.com/davecgh/dcrstakesim/internal/tickettreap"
)

var s1 float64

func (s *simulator) calcNextStakeDiffProposalJ() int64 {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int32(0)
	if s.tip != nil {
		nextHeight = s.tip.height + 1
	}
	altMinDiff := int64(4) * 1e8 // normally s.params.MinimumStakeDiff
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return altMinDiff
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// The following variables are used in the algorithm
	// Pool: p, c, t (previous, current, target)
	// Price : q, curDiff, n (previous, current, next)

	// Pool
	c := int64(s.tip.poolSize)
	t := int64(s.params.TicketsPerBlock) * int64(s.params.TicketPoolSize)
	// Previous window pool size and ticket price
	p, q := s.poolSizeAndDiff(s.tip.height - int32(intervalSize))
	// Return the existing ticket price for the first interval.
	if p == 0 {
		return curDiff
	}

	// Useful ticket counts are A (-5 * 144) and B (15 * 144)
	// A := -int64(s.params.TicketsPerBlock) * intervalSize
	// B := (int64(s.params.MaxFreshStakePerBlock) - int64(s.params.TicketsPerBlock)) * intervalSize

	// Pool velocity (not used in this version)
	// poolDelta := c - p
	// slowDown := 1 - math.Abs(float64(poolDelta)) / float64(B+A)

	// Pool force (multiple of target pool size, signed)
	del := float64(c-t) / float64(t)
	// del /= float64(s.params.MaxFreshStakePerBlock)

	// Price velocity damper (always positive) - A large change in price from
	// the previous window to the current will have the effect of attenuating
	// the price change we are computing here. This is a very simple way of
	// slowing down the price, at the expense of making the price a little jumpy
	// and slower to adapt to big events.
	//
	// Magnitude of price change as a percent of previous price.
	absPriceDeltaLast := math.Abs(float64(curDiff-q) / float64(q))
	// Mapped onto (0,1] by an exponential decay
	m := math.Exp(-absPriceDeltaLast * 8)
	// NOTE: make this stochastic by replacing the number 8 something like
	// (rand.NewSource(s.tip.ticketPrice).Int63() >> 59)

	// Scale directional (signed) pool force with the exponentially-mapped price
	// derivative. Interpret the scalar input parameter as a percent of this
	// computed price delta.
	pctChange := s1 / 100 * m * del
	n := float64(curDiff) * (1.0 + pctChange)

	// Enforce minimum price
	price := int64(n)
	if price < altMinDiff {
		price = altMinDiff
	}

	// Verbose info
	fmt.Println(c, c-t, m, curDiff, q, absPriceDeltaLast, pctChange, price)

	return price
}

func (s *simulator) poolSizeAndDiff(height int32) (int64, int64) {
	node := s.ancestorNode(s.tip, height, nil)
	if node != nil {
		return int64(node.poolSize), node.ticketPrice
	}
	return int64(s.tip.poolSize), s.tip.ticketPrice
}

// calcNextStakeDiffProposal1 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by raedah in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal1() int64 {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int32(0)
	if s.tip != nil {
		nextHeight = s.tip.height + 1
	}
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return s.params.MinimumStakeDiff
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// Attempt to get the pool size from the previous retarget interval.
	var prevPoolSize int64
	prevRetargetHeight := nextHeight - int32(intervalSize)
	node := s.ancestorNode(s.tip, prevRetargetHeight, nil)
	if node != nil {
		prevPoolSize = int64(node.poolSize)
	}

	// Return the existing ticket price for the first interval.
	if prevPoolSize == 0 {
		return curDiff
	}

	curPoolSize := int64(s.tip.poolSize)
	ratio := float64(curPoolSize) / float64(prevPoolSize)
	return int64(float64(curDiff) * ratio)
}

// calcNextStakeDiffProposal2 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by animedow in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal2() int64 {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int32(0)
	if s.tip != nil {
		nextHeight = s.tip.height + 1
	}
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return s.params.MinimumStakeDiff
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	//                ax
	// f(x) = - ---------------- + d
	//           (x - b)(x + c)
	//
	// x = amount of ticket deviation from the target pool size;
	// a = a modifier controlling the slope of the function;
	// b = the maximum boundary;
	// c = the minimum boundary;
	// d = the average ticket price in pool.
	x := int64(s.tip.poolSize) - (int64(s.params.TicketsPerBlock) *
		int64(s.params.TicketPoolSize))
	a := int64(100000)
	b := int64(2880)
	c := int64(2880)
	var d int64
	var totalSpent int64
	totalTickets := int64(len(s.immatureTickets) + s.liveTickets.Len())
	if totalTickets != 0 {
		for _, ticket := range s.immatureTickets {
			totalSpent += int64(ticket.price)
		}
		s.liveTickets.ForEach(func(k tickettreap.Key, v *tickettreap.Value) bool {
			totalSpent += v.PurchasePrice
			return true
		})
		d = totalSpent / totalTickets
	}
	price := int64(float64(d) - 100000000*(float64(a*x)/float64((x-b)*(x+c))))
	if price < s.params.MinimumStakeDiff {
		price = s.params.MinimumStakeDiff
	}
	return price
}

// calcNextStakeDiffProposal3 returns the required stake difficulty (aka ticket
// price) for the block after the current tip block the simulator is associated
// with using the algorithm proposed by coblee in
// https://github.com/decred/dcrd/issues/584
func (s *simulator) calcNextStakeDiffProposal3() int64 {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int32(0)
	if s.tip != nil {
		nextHeight = s.tip.height + 1
	}
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return s.params.MinimumStakeDiff
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// f(x) = x*(locked/target_pool_size) + (1-x)*(locked/pool_size_actual)
	ticketsPerBlock := int64(s.params.TicketsPerBlock)
	targetPoolSize := ticketsPerBlock * int64(s.params.TicketPoolSize)
	lockedSupply := s.tip.stakedCoins
	x := int64(1)
	var price int64
	if s.tip.poolSize == 0 {
		price = int64(lockedSupply) / targetPoolSize
	} else {
		price = x*int64(lockedSupply)/targetPoolSize +
			(1-x)*(int64(lockedSupply)/int64(s.tip.poolSize))
	}
	if price < s.params.MinimumStakeDiff {
		price = s.params.MinimumStakeDiff
	}
	return price
}
