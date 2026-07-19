package slices

func Map[T any, U any](s []T, f func(T) U) []U {
	if s == nil {
		return nil
	}

	newS := make([]U, len(s))
	for i, item := range s {
		newS[i] = f(item)
	}
	return newS
}
