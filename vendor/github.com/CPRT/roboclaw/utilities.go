//Created by Michael Dysart
package roboclaw

/*
 * Formats a uint16 into a big endian byte slice
 * @param val {uint16}
 * @return {[]uint8}
 */
func setWORDval(val uint16) []uint8 {
	return []uint8{uint8(val >> 8), uint8(val)}
}

/*
 * Formats a uint32 into a big endian byte slice
 * @param val {uint32}
 * @return {[]uint8}
 */
func setDWORDval(val uint32) []uint8 {
	return []uint8{uint8(val >> 24), uint8(val >> 16), uint8(val >> 8), uint8(val)}
}
