package utils

import "math/rand"

func PickOne[T any](items []T) T {
	return items[rand.Intn(len(items))]
}
