package utils

import "math/rand"

// PickOne returns a random element from items. The second return value is false if items is empty.
func PickOne[T any](items []T) (T, bool) {
	var zero T
	if len(items) == 0 {
		return zero, false
	}
	return items[rand.Intn(len(items))], true
}
