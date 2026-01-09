package log

import (
	"context"
	"errors"
)

var ErrLogTooDeep = errors.New("log chain too deep")

type LoadFunc[T any] func(ctx context.Context, cid string) (*T, error)

type EntryWithCID[T any] struct {
	CID            string
	Value          *T
	ValidSignature bool
}

func FetchChain[T any](
	ctx context.Context,
	headCID *string,
	load LoadFunc[T],
	prevCID func(*T) *string,
	verify func(*T) bool,
	maxDepth int,
) ([]EntryWithCID[T], error) {
	if headCID == nil || *headCID == "" {
		return nil, nil
	}
	if maxDepth <= 0 {
		maxDepth = 10_000
	}

	visited := make(map[string]struct{})
	var out []EntryWithCID[T]

	current := headCID
	for current != nil && *current != "" {
		cid := *current
		if _, ok := visited[cid]; ok {
			break
		}
		if len(out) >= maxDepth {
			return nil, ErrLogTooDeep
		}
		visited[cid] = struct{}{}

		v, err := load(ctx, cid)
		if err != nil {
			return nil, err
		}
		out = append(out, EntryWithCID[T]{CID: cid, Value: v, ValidSignature: verify(v)})
		current = prevCID(v)
	}

	reverse(out)
	return out, nil
}

func reverse[T any](s []EntryWithCID[T]) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
