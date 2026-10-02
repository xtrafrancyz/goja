//go:build goja_stats

package goja

import "sync/atomic"

type statsStruct struct {
	tinyClassTotal, tinyClassMisses, tinyObjectCreates, tinyObjectDeoptimizations atomic.Uint32

	icProtoHitCount, icProtoUncacheableHitCount, icHitCount, icMissCount, icMegaCount, icProtoEpochUpdateCount atomic.Uint32

	tinyClassMultiTransitions atomic.Uint32
}

var stats statsStruct

func (s *statsStruct) incTinyClassTotal() {
	stats.tinyClassTotal.Add(1)
}

func (s *statsStruct) incTinyClassMisses() {
	stats.tinyClassMisses.Add(1)
}

func (s *statsStruct) incTinyObjectCreates() {
	stats.tinyObjectCreates.Add(1)
}

func (s *statsStruct) incTinyObjectDeoptimizations() {
	stats.tinyObjectDeoptimizations.Add(1)
}

func (s *statsStruct) incTinyClassMultiTransitions() {
	stats.tinyClassMultiTransitions.Add(1)
}

func (s *statsStruct) incICProtoHitCount() {
	stats.icProtoHitCount.Add(1)
}

func (s *statsStruct) incICProtoUncacheableHitCount() {
	stats.icProtoUncacheableHitCount.Add(1)
}

func (s *statsStruct) incICHitCount() {
	stats.icHitCount.Add(1)
}

func (s *statsStruct) incICMissCount() {
	stats.icMissCount.Add(1)
}

func (s *statsStruct) incICMegaCount() {
	stats.icMegaCount.Add(1)
}

func (s *statsStruct) incICProtoEpochUpdateCount() {
	stats.icProtoEpochUpdateCount.Add(1)
}

func printStats(printf func(string, ...any)) {
	tinyObjectMisses := stats.tinyClassMisses.Load()
	tinyObjectTotal := stats.tinyClassTotal.Load()
	tinyObjectCreates := stats.tinyObjectCreates.Load()
	tinyObjectDeoptimizations := stats.tinyObjectDeoptimizations.Load()
	tinyClassMultiTransitions := stats.tinyClassMultiTransitions.Load()
	icProtoHitCount := stats.icProtoHitCount.Load()
	icProtoUncacheableHitCount := stats.icProtoUncacheableHitCount.Load()
	icHitCount := stats.icHitCount.Load()
	icMissCount := stats.icMissCount.Load()
	icMegaCount := stats.icMegaCount.Load()
	icProtoEpochUpdateCount := stats.icProtoEpochUpdateCount.Load()

	printf("tinyObject miss ratio: %.02f%% (%d/%d)", float64(tinyObjectMisses)/float64(tinyObjectTotal)*100, tinyObjectMisses, tinyObjectTotal)
	printf("tinyObject deoptimizations ratio: %.02f%% (%d/%d)", float64(tinyObjectDeoptimizations)/float64(tinyObjectCreates)*100, tinyObjectDeoptimizations, tinyObjectCreates)
	printf("tinyClass multi property transitions: %d", tinyClassMultiTransitions)
	printf("IC stats: proto hit=%d, proto uncacheable hit=%d, hit=%d, miss=%d, mega=%d, proto epoch update=%d",
		icProtoHitCount,
		icProtoUncacheableHitCount,
		icHitCount,
		icMissCount,
		icMegaCount,
		icProtoEpochUpdateCount)
}

func clearStats() {
	stats = statsStruct{}
}
