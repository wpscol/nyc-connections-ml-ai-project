package embeddings

import (
	"math"
	"math/rand"
	"sort"
)

// balancedAssign distributes n points into k clusters such that each cluster
// holds either floor(n/k) or ceil(n/k) members. Points are assigned greedily
// in descending similarity order so the best (point,cluster) matches are
// honoured first. This prevents degenerate singleton or empty clusters.
func balancedAssign(normed [][]float64, centroids [][]float64) []int {
	n := len(normed)
	k := len(centroids)
	floor := n / k
	// first (n mod k) clusters get one extra slot
	extra := n % k

	type pair struct {
		i, c int
		sim  float64
	}
	pairs := make([]pair, 0, n*k)
	for i, v := range normed {
		for c, cent := range centroids {
			pairs = append(pairs, pair{i, c, dotProd(v, cent)})
		}
	}
	sort.Slice(pairs, func(a, b int) bool { return pairs[a].sim > pairs[b].sim })

	assign := make([]int, n)
	for i := range assign {
		assign[i] = -1
	}
	counts := make([]int, k)
	assigned := 0

	for _, p := range pairs {
		if assigned == n {
			break
		}
		if assign[p.i] != -1 {
			continue
		}
		cap := floor
		if p.c < extra {
			cap = floor + 1
		}
		if counts[p.c] < cap {
			assign[p.i] = p.c
			counts[p.c]++
			assigned++
		}
	}
	// Safety: assign any stragglers to the least-full cluster
	for i, a := range assign {
		if a != -1 {
			continue
		}
		minC := 0
		for c := 1; c < k; c++ {
			if counts[c] < counts[minC] {
				minC = c
			}
		}
		assign[i] = minC
		counts[minC]++
	}
	return assign
}

const (
	clusterRestarts = 20
	clusterMaxIter  = 150
)

// Group is one cluster returned by SuggestGroups.
type Group struct {
	Rank          int      `json:"rank"`
	Words         []string `json:"words"`
	AvgSimilarity float64  `json:"avg_similarity"` // mean pairwise cosine similarity within group
	MinSimilarity float64  `json:"min_similarity"` // weakest pairwise cosine similarity (cohesion floor)
	Confidence    float64  `json:"confidence"`     // composite quality score 0–1
}

// SuggestResult is the full output of SuggestGroups.
type SuggestResult struct {
	Groups         []Group `json:"suggested_groups"`
	OverallQuality float64 `json:"overall_quality"` // silhouette score: -1 (bad) to 1 (perfect)
	EmbeddingModel string  `json:"embedding_model"`
}

// SuggestGroups clusters words into k groups using K-means++ on cosine similarity.
// It runs clusterRestarts independent starts and returns the best by silhouette score.
func SuggestGroups(words []string, vecs [][]float64, k int, model string) SuggestResult {
	n := len(words)
	if n == 0 || k <= 0 || k > n {
		return SuggestResult{EmbeddingModel: model}
	}

	normed := make([][]float64, n)
	for i, v := range vecs {
		normed[i] = normalizeVec(v)
	}

	bestSil := math.Inf(-1)
	var bestAssign []int

	for r := 0; r < clusterRestarts; r++ {
		assign := kmeansRun(normed, k)
		s := silhouetteScore(normed, assign, k)
		if s > bestSil {
			bestSil = s
			bestAssign = make([]int, n)
			copy(bestAssign, assign)
		}
	}

	groups := buildGroups(words, normed, bestAssign, k)

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Confidence > groups[j].Confidence
	})
	for i := range groups {
		groups[i].Rank = i + 1
	}

	return SuggestResult{
		Groups:         groups,
		OverallQuality: roundF(bestSil, 3),
		EmbeddingModel: model,
	}
}

// ── K-means ──────────────────────────────────────────────────────────────────

func kmeansRun(normed [][]float64, k int) []int {
	dim := len(normed[0])
	centroids := kmeanspp(normed, k)
	assign := balancedAssign(normed, centroids)

	for iter := 0; iter < clusterMaxIter; iter++ {
		// Recompute centroids
		sums := make([][]float64, k)
		counts := make([]int, k)
		for c := range sums {
			sums[c] = make([]float64, dim)
		}
		for i, c := range assign {
			for d, x := range normed[i] {
				sums[c][d] += x
			}
			counts[c]++
		}
		for c := range centroids {
			if counts[c] > 0 {
				centroids[c] = normalizeVec(sums[c])
			}
		}

		newAssign := balancedAssign(normed, centroids)

		changed := false
		for i := range assign {
			if assign[i] != newAssign[i] {
				changed = true
				break
			}
		}
		assign = newAssign
		if !changed {
			break
		}
	}
	return assign
}

// kmeanspp picks k initial centroids using the K-means++ probability distribution.
func kmeanspp(normed [][]float64, k int) [][]float64 {
	n := len(normed)
	cs := make([][]float64, 0, k)
	cs = append(cs, normed[rand.Intn(n)])

	for len(cs) < k {
		dists := make([]float64, n)
		total := 0.0
		for i, v := range normed {
			minD := math.Inf(1)
			for _, c := range cs {
				if d := 1.0 - dotProd(v, c); d < minD {
					minD = d
				}
			}
			dists[i] = minD * minD
			total += dists[i]
		}
		if total == 0 {
			cs = append(cs, normed[rand.Intn(n)])
			continue
		}
		r := rand.Float64() * total
		cumul := 0.0
		chosen := n - 1
		for i, d := range dists {
			cumul += d
			if cumul >= r {
				chosen = i
				break
			}
		}
		cs = append(cs, normed[chosen])
	}
	return cs
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func buildGroups(words []string, normed [][]float64, assign []int, k int) []Group {
	cWords := make([][]string, k)
	cVecs := make([][][]float64, k)
	for i, c := range assign {
		cWords[c] = append(cWords[c], words[i])
		cVecs[c] = append(cVecs[c], normed[i])
	}

	groups := make([]Group, 0, k)
	for c := 0; c < k; c++ {
		if len(cWords[c]) == 0 {
			continue
		}
		avg, minSim := pairwiseSims(cVecs[c])
		// Confidence weights avg heavily but penalises weak-link members via min
		conf := roundF(0.65*avg+0.35*minSim, 3)
		groups = append(groups, Group{
			Words:         cWords[c],
			AvgSimilarity: roundF(avg, 3),
			MinSimilarity: roundF(minSim, 3),
			Confidence:    conf,
		})
	}
	return groups
}

// pairwiseSims returns the mean and minimum cosine similarity over all pairs.
func pairwiseSims(vecs [][]float64) (avg, minSim float64) {
	if len(vecs) <= 1 {
		return 1.0, 1.0
	}
	sum, minVal := 0.0, math.Inf(1)
	count := 0
	for i := 0; i < len(vecs); i++ {
		for j := i + 1; j < len(vecs); j++ {
			s := dotProd(vecs[i], vecs[j])
			sum += s
			if s < minVal {
				minVal = s
			}
			count++
		}
	}
	if count == 0 {
		return 1, 1
	}
	return sum / float64(count), minVal
}

// silhouetteScore computes the mean silhouette coefficient using cosine distance.
// Range: -1 (wrong cluster) to 0 (boundary) to 1 (tight, well-separated clusters).
func silhouetteScore(normed [][]float64, assign []int, k int) float64 {
	n := len(normed)
	if n == 0 {
		return 0
	}
	total := 0.0
	for i := range normed {
		ci := assign[i]

		// a(i): mean distance to same-cluster members
		aSum, aCount := 0.0, 0
		for j := range normed {
			if j != i && assign[j] == ci {
				aSum += 1.0 - dotProd(normed[i], normed[j])
				aCount++
			}
		}
		a := 0.0
		if aCount > 0 {
			a = aSum / float64(aCount)
		}

		// b(i): mean distance to nearest other cluster
		b := math.Inf(1)
		for c := 0; c < k; c++ {
			if c == ci {
				continue
			}
			bSum, bCount := 0.0, 0
			for j := range normed {
				if assign[j] == c {
					bSum += 1.0 - dotProd(normed[i], normed[j])
					bCount++
				}
			}
			if bCount > 0 {
				if mean := bSum / float64(bCount); mean < b {
					b = mean
				}
			}
		}
		if math.IsInf(b, 1) {
			b = 0
		}

		if denom := math.Max(a, b); denom > 0 {
			total += (b - a) / denom
		}
	}
	return total / float64(n)
}

// ── Math helpers ──────────────────────────────────────────────────────────────

func normalizeVec(v []float64) []float64 {
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	out := make([]float64, len(v))
	if norm < 1e-10 {
		copy(out, v)
		return out
	}
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

func dotProd(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	s := 0.0
	for i := 0; i < n; i++ {
		s += a[i] * b[i]
	}
	return s
}

func roundF(f float64, decimals int) float64 {
	p := math.Pow10(decimals)
	return math.Round(f*p) / p
}
