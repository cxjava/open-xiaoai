package utils

import "math/rand/v2"

// PickOne returns a random element from items. The second return value is false if items is empty.
func PickOne[T any](items []T) (T, bool) {
	var zero T
	if len(items) == 0 {
		return zero, false
	}
	return items[rand.IntN(len(items))], true
}
