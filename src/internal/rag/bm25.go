//go:build windows

package rag

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Bm25Result is a ranked search hit from the BM25 index.
type Bm25Result struct {
	DocID string
	Score float64
}

// Bm25Index is a lightweight BM25 inverted index for keyword retrieval.
// It provides sparse retrieval complementary to the chromem-go dense vector store.
type Bm25Index struct {
	k1, b float64
	docs  map[string]*bm25Doc // docID → doc
	terms map[string]*bm25Term // term → posting list
}

type bm25Doc struct {
	id     string
	tokens []string
	length int
}

type bm25Term struct {
	postings map[string]int // docID → term frequency
	df       int            // document frequency
}

// NewBm25Index creates a BM25 index with standard parameters (k1=1.5, b=0.75).
func NewBm25Index() *Bm25Index {
	return &Bm25Index{
		k1:    1.5,
		b:     0.75,
		docs:  make(map[string]*bm25Doc),
		terms: make(map[string]*bm25Term),
	}
}

// Add indexes a document. Subsequent calls with the same docID update the entry.
func (idx *Bm25Index) Add(docID, content string) {
	// Remove old entry if present
	idx.Remove(docID)

	tokens := tokenize(content)
	doc := &bm25Doc{id: docID, tokens: tokens, length: len(tokens)}
	idx.docs[docID] = doc

	// Count term frequencies (deduplicated across a single doc)
	seen := make(map[string]int)
	for _, tok := range tokens {
		seen[tok]++
	}
	for tok, tf := range seen {
		t, ok := idx.terms[tok]
		if !ok {
			t = &bm25Term{postings: make(map[string]int)}
			idx.terms[tok] = t
		}
		t.postings[docID] = tf
		t.df++ // doc already removed above, so this is safe
	}
}

// Remove deletes a document from the index.
func (idx *Bm25Index) Remove(docID string) {
	doc, ok := idx.docs[docID]
	if !ok {
		return
	}
	// Decrement df for each unique term in this document
	seen := make(map[string]bool, len(doc.tokens))
	for _, tok := range doc.tokens {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		if t, ok := idx.terms[tok]; ok {
			delete(t.postings, docID)
			t.df--
			if t.df <= 0 {
				delete(idx.terms, tok)
			}
		}
	}
	delete(idx.docs, docID)
}

// Search runs a BM25 keyword search and returns the top-k results sorted by score descending.
func (idx *Bm25Index) Search(query string, k int) []Bm25Result {
	if len(idx.docs) == 0 || k <= 0 {
		return nil
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	n := float64(len(idx.docs))
	avgdl := idx.avgDocLength()

	scores := make(map[string]float64)

	for _, qt := range queryTokens {
		t, ok := idx.terms[qt]
		if !ok {
			continue
		}
		idf := math.Log((n-float64(t.df)+0.5)/(float64(t.df)+0.5) + 1.0)
		for docID, tf := range t.postings {
			doc := idx.docs[docID]
			dl := float64(doc.length)
			num := float64(tf) * (idx.k1 + 1)
			den := float64(tf) + idx.k1*(1-idx.b+idx.b*dl/avgdl)
			scores[docID] += idf * num / den
		}
	}

	results := make([]Bm25Result, 0, len(scores))
	for docID, score := range scores {
		results = append(results, Bm25Result{DocID: docID, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > k {
		results = results[:k]
	}
	return results
}

// Len returns the number of indexed documents.
func (idx *Bm25Index) Len() int {
	return len(idx.docs)
}

// avgDocLength computes the mean document token count across the index.
func (idx *Bm25Index) avgDocLength() float64 {
	if len(idx.docs) == 0 {
		return 1.0
	}
	var total int
	for _, d := range idx.docs {
		total += d.length
	}
	return float64(total) / float64(len(idx.docs))
}

// ─── Tokenizer ───────────────────────────────────────────────────

// tokenize splits text into lowercase tokens. For Latin text it splits on
// non-letter boundaries. For CJK it emits each character as a unigram token,
// plus bigrams for compound matching.
func tokenize(text string) []string {
	var tokens []string
	runes := []rune(text)

	i := 0
	for i < len(runes) {
		r := runes[i]
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			// Collect a run of letters/digits
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			token := strings.ToLower(string(runes[start:i]))
			if len(token) > 0 {
				tokens = append(tokens, token)
			}
		} else if isCJK(r) {
			// CJK character: emit as unigram
			tokens = append(tokens, string(r))
			// Also emit bigram with previous CJK char
			if i > 0 && isCJK(runes[i-1]) {
				tokens = append(tokens, string([]rune{runes[i-1], r}))
			}
			i++
		} else {
			i++
		}
	}

	// Deduplicate while preserving order (for query-side tokenization)
	seen := make(map[string]bool, len(tokens))
	uniq := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if len(t) <= 1 && !isCJK([]rune(t)[0]) {
			continue // skip single Latin chars
		}
		if !seen[t] {
			seen[t] = true
			uniq = append(uniq, t)
		}
	}
	return uniq
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x3000 && r <= 0x303F) // CJK Symbols
}
