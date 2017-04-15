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
	altMinDiff := int64(4)
	stakeDiffStartHeight := int32(s.params.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return altMinDiff * 1e8 // s.params.MinimumStakeDiff
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := s.params.StakeDiffWindowSize
	curDiff := s.tip.ticketPrice
	if int64(nextHeight)%intervalSize != 0 {
		return curDiff
	}

	// Pool: p, c, t (previous, current, target)
	// Price : q, cur, n (previous, current, NEXT)

	// Pool
	c := int64(s.tip.poolSize)
	t := int64(s.params.TicketsPerBlock) * int64(s.params.TicketPoolSize)
	// Previous window pool size and ticket price
	p, q := s.previousPoolSizeAndDiff(nextHeight)
	// Return the existing ticket price for the first interval.
	if p == 0 {
		return curDiff
	}

	// Pool velocity (normalized, always non-negative)
	//A := -int64(s.params.TicketsPerBlock) * intervalSize
	//B := (int64(s.params.MaxFreshStakePerBlock) - int64(s.params.TicketsPerBlock)) * intervalSize
	//D := c - p
	//slowDown := (1 - math.Abs(float64(D)) / float64(B+A))

	// Pool force (multiple of target, signed)
	del := float64(c-t) / float64(t) / float64(s.params.MaxFreshStakePerBlock)

	// Price damper (always positive)
	absPriceDeltaLast := math.Abs(float64(curDiff-q) / float64(q))
	m := s1 * math.Exp(-absPriceDeltaLast)

	// Adjust
	pctChange := m * del
	// if pctChange > 1.5 {
	// 	pctChange = 1.5
	// } else if pctChange < -0.5 {
	// 	pctChange = -0.5
	// }
	n := float64(curDiff) * (1.0 + pctChange)

	price := int64(n)
	if price < altMinDiff*1e8 /* s.params.MinimumStakeDiff */ {
		price = altMinDiff * 1e8 // s.params.MinimumStakeDiff
	}

	fmt.Println(c, c-t, m*del*float64(curDiff), pctChange, price)

	return price
}

func (s *simulator) previousPoolSizeAndDiff(nextHeight int32) (int64, int64) {
	node := s.ancestorNode(s.tip, nextHeight-int32(s.params.StakeDiffWindowSize), nil)
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
