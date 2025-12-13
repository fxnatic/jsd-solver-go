package utils

import "strings"

type LZString struct {
	keyStr string
}

func NewLZString(keyStr string) *LZString {
	return &LZString{
		keyStr: keyStr,
	}
}

func (lz *LZString) CompressToBase64(input string) string {
	if input == "" {
		return ""
	}
	res := lz.compress(input, 6, func(a int) string {
		return string(lz.keyStr[a])
	})
	switch len(res) % 4 {
	case 0:
		return res
	case 1:
		return res + "==="
	case 2:
		return res + "=="
	case 3:
		return res + "="
	}
	return res
}

func (lz *LZString) compress(uncompressed string, bitsPerChar int, getCharFromInt func(int) string) string {
	if uncompressed == "" {
		return ""
	}

	var (
		contextDictionary         = make(map[string]int)
		contextDictionaryToCreate = make(map[string]bool)
		contextC                  = ""
		contextWC                 = ""
		contextW                  = ""
		contextEnlargeIn          = 2
		contextDictSize           = 3
		contextNumBits            = 2
		contextData               strings.Builder
		contextDataVal            = 0
		contextDataPosition       = 0
	)

	for i := 0; i < len(uncompressed); i++ {
		contextC = string(uncompressed[i])
		if _, ok := contextDictionary[contextC]; !ok {
			contextDictionary[contextC] = contextDictSize
			contextDictSize++
			contextDictionaryToCreate[contextC] = true
		}

		contextWC = contextW + contextC
		if _, ok := contextDictionary[contextWC]; ok {
			contextW = contextWC
		} else {
			if _, ok := contextDictionaryToCreate[contextW]; ok {
				if len(contextW) > 0 && int(contextW[0]) < 256 {
					for i := 0; i < contextNumBits; i++ {
						contextDataVal = contextDataVal << 1
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextData.WriteString(getCharFromInt(contextDataVal))
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
					}
					value := int(contextW[0])
					for i := 0; i < 8; i++ {
						contextDataVal = (contextDataVal << 1) | (value & 1)
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextData.WriteString(getCharFromInt(contextDataVal))
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
						value = value >> 1
					}
				} else {
					value := 1
					for i := 0; i < contextNumBits; i++ {
						contextDataVal = (contextDataVal << 1) | value
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextData.WriteString(getCharFromInt(contextDataVal))
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
						value = 0
					}
					value = int(contextW[0])
					for i := 0; i < 16; i++ {
						contextDataVal = (contextDataVal << 1) | (value & 1)
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextData.WriteString(getCharFromInt(contextDataVal))
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
						value = value >> 1
					}
				}
				contextEnlargeIn--
				if contextEnlargeIn == 0 {
					contextEnlargeIn = 1 << contextNumBits
					contextNumBits++
				}
				delete(contextDictionaryToCreate, contextW)
			} else {
				value := contextDictionary[contextW]
				for i := 0; i < contextNumBits; i++ {
					contextDataVal = (contextDataVal << 1) | (value & 1)
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextData.WriteString(getCharFromInt(contextDataVal))
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value = value >> 1
				}
			}
			contextEnlargeIn--
			if contextEnlargeIn == 0 {
				contextEnlargeIn = 1 << contextNumBits
				contextNumBits++
			}
			contextDictionary[contextWC] = contextDictSize
			contextDictSize++
			contextW = contextC
		}
	}

	if contextW != "" {
		if _, ok := contextDictionaryToCreate[contextW]; ok {
			if len(contextW) > 0 && int(contextW[0]) < 256 {
				for i := 0; i < contextNumBits; i++ {
					contextDataVal = contextDataVal << 1
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextData.WriteString(getCharFromInt(contextDataVal))
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
				}
				value := int(contextW[0])
				for i := 0; i < 8; i++ {
					contextDataVal = (contextDataVal << 1) | (value & 1)
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextData.WriteString(getCharFromInt(contextDataVal))
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value = value >> 1
				}
			} else {
				value := 1
				for i := 0; i < contextNumBits; i++ {
					contextDataVal = (contextDataVal << 1) | value
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextData.WriteString(getCharFromInt(contextDataVal))
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value = 0
				}
				value = int(contextW[0])
				for i := 0; i < 16; i++ {
					contextDataVal = (contextDataVal << 1) | (value & 1)
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextData.WriteString(getCharFromInt(contextDataVal))
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value = value >> 1
				}
			}
			contextEnlargeIn--
			if contextEnlargeIn == 0 {
				contextEnlargeIn = 1 << contextNumBits
				contextNumBits++
			}
			delete(contextDictionaryToCreate, contextW)
		} else {
			value := contextDictionary[contextW]
			for i := 0; i < contextNumBits; i++ {
				contextDataVal = (contextDataVal << 1) | (value & 1)
				if contextDataPosition == bitsPerChar-1 {
					contextDataPosition = 0
					contextData.WriteString(getCharFromInt(contextDataVal))
					contextDataVal = 0
				} else {
					contextDataPosition++
				}
				value = value >> 1
			}
		}
		contextEnlargeIn--
		if contextEnlargeIn == 0 {
			contextNumBits++
		}
	}

	value := 2
	for i := 0; i < contextNumBits; i++ {
		contextDataVal = (contextDataVal << 1) | (value & 1)
		if contextDataPosition == bitsPerChar-1 {
			contextDataPosition = 0
			contextData.WriteString(getCharFromInt(contextDataVal))
			contextDataVal = 0
		} else {
			contextDataPosition++
		}
		value = value >> 1
	}

	for {
		contextDataVal = contextDataVal << 1
		if contextDataPosition == bitsPerChar-1 {
			contextData.WriteString(getCharFromInt(contextDataVal))
			break
		} else {
			contextDataPosition++
		}
	}

	return contextData.String()
}

func (lz *LZString) DecompressFromBase64(input string) string {
	if input == "" {
		return ""
	}

	input = strings.TrimRight(input, "=")

	return lz.decompress(len(input), 32, func(index int) int {
		if index >= len(input) {
			return -1
		}
		return strings.IndexByte(lz.keyStr, input[index])
	})
}

func (lz *LZString) DecompressFromEncodedURIComponent(input string) string {
	if input == "" {
		return ""
	}

	keyStrUriSafe := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+-$"

	input = strings.ReplaceAll(input, " ", "+")

	return lz.decompress(len(input), 32, func(index int) int {
		if index >= len(input) {
			return -1
		}
		return strings.IndexByte(keyStrUriSafe, input[index])
	})
}

func (lz *LZString) DecompressFromCloudflare(input string) string {
	if input == "" {
		return ""
	}

	cfCharset := "Mz8g3qloHTIEuWaYsw9j56Sc47Dpbx0GJ-kO2AvfyQLnirmFeRtC$K+PUdh1VXZBN"

	return lz.decompress(len(input), 32, func(index int) int {
		if index >= len(input) {
			return -1
		}
		return strings.IndexByte(cfCharset, input[index])
	})
}

func (lz *LZString) decompress(length int, resetValue int, getNextValue func(int) int) string {
	dictionary := make([]string, 0)
	enlargeIn := 4
	dictSize := 4
	numBits := 3
	var result strings.Builder

	dataVal := getNextValue(0)
	dataPosition := resetValue
	dataIndex := 1

	for i := 0; i < 3; i++ {
		dictionary = append(dictionary, string(rune(i)))
	}

	bits := 0
	maxpower := 4
	power := 1

	for power != maxpower {
		resb := dataVal & dataPosition
		dataPosition >>= 1
		if dataPosition == 0 {
			dataPosition = resetValue
			dataVal = getNextValue(dataIndex)
			dataIndex++
		}
		if resb > 0 {
			bits |= power
		}
		power <<= 1
	}

	var c string
	switch bits {
	case 0:
		bits = 0
		maxpower = 256
		power = 1
		for power != maxpower {
			resb := dataVal & dataPosition
			dataPosition >>= 1
			if dataPosition == 0 {
				dataPosition = resetValue
				dataVal = getNextValue(dataIndex)
				dataIndex++
			}
			if resb > 0 {
				bits |= power
			}
			power <<= 1
		}
		c = string(rune(bits))
	case 1:
		bits = 0
		maxpower = 65536
		power = 1
		for power != maxpower {
			resb := dataVal & dataPosition
			dataPosition >>= 1
			if dataPosition == 0 {
				dataPosition = resetValue
				dataVal = getNextValue(dataIndex)
				dataIndex++
			}
			if resb > 0 {
				bits |= power
			}
			power <<= 1
		}
		c = string(rune(bits))
	case 2:
		return ""
	}

	dictionary = append(dictionary, c)
	w := c
	result.WriteString(c)

	for {
		if dataIndex > length {
			return ""
		}

		bits = 0
		maxpower = 1 << numBits
		power = 1

		for power != maxpower {
			resb := dataVal & dataPosition
			dataPosition >>= 1
			if dataPosition == 0 {
				dataPosition = resetValue
				dataVal = getNextValue(dataIndex)
				dataIndex++
			}
			if resb > 0 {
				bits |= power
			}
			power <<= 1
		}

		cInt := bits
		switch cInt {
		case 0:
			bits = 0
			maxpower = 256
			power = 1
			for power != maxpower {
				resb := dataVal & dataPosition
				dataPosition >>= 1
				if dataPosition == 0 {
					dataPosition = resetValue
					dataVal = getNextValue(dataIndex)
					dataIndex++
				}
				if resb > 0 {
					bits |= power
				}
				power <<= 1
			}
			dictionary = append(dictionary, string(rune(bits)))
			dictSize++
			cInt = dictSize - 1
			enlargeIn--
		case 1:
			bits = 0
			maxpower = 65536
			power = 1
			for power != maxpower {
				resb := dataVal & dataPosition
				dataPosition >>= 1
				if dataPosition == 0 {
					dataPosition = resetValue
					dataVal = getNextValue(dataIndex)
					dataIndex++
				}
				if resb > 0 {
					bits |= power
				}
				power <<= 1
			}
			dictionary = append(dictionary, string(rune(bits)))
			dictSize++
			cInt = dictSize - 1
			enlargeIn--
		case 2:
			return result.String()
		}

		if enlargeIn == 0 {
			enlargeIn = 1 << numBits
			numBits++
		}

		var entry string
		if cInt < len(dictionary) {
			entry = dictionary[cInt]
		} else if cInt == dictSize {
			entry = w + string(w[0])
		} else {
			return ""
		}

		result.WriteString(entry)

		dictionary = append(dictionary, w+string(entry[0]))
		dictSize++

		enlargeIn--
		if enlargeIn == 0 {
			enlargeIn = 1 << numBits
			numBits++
		}

		w = entry
	}
}
