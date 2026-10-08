package tilemap

func iabs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func isign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}
