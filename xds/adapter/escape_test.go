/*
Copyright 2018 Turbine Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package adapter

import (
	"fmt"
	"testing"
	"unicode"

	"github.com/turbinelabs/test/assert"
)

func TestTransformerConfigurationsAndInvariants(t *testing.T) {
	// ***************************************************************
	// If something in this test fails, the contract between
	// transformer.transform and the configured len and replacement
	// methods may have been violated. This may result in runtime
	// panics in XDS.
	// ***************************************************************

	// Metadata escaping never produces regular expressions and
	// therefore does not allow them to be escaped.
	assert.Equal(t, metadataEscaper.mode, noEscape)
	assert.Panic(t, func() { metadataEscapeLen('*', dynamicEscape) })
	assert.Panic(t, func() { metadataEscapeLen('*', alwaysEscape) })
	assert.Panic(t, func() { metadataEscape('*', true) })
	for b := 0; b < 0x100; b++ {
		assert.Group(fmt.Sprintf("metadata input: 0x%x", b), t, func(g *assert.G) {
			n, encoding := metadataEscapeLen(byte(b), noEscape)
			assert.GreaterThanEqual(g, n, 1)
			assert.LessThanEqual(g, n, 3)
			assert.NotEqual(g, n, 2)
			assert.NotEqual(g, encoding, regexEncoded)

			assert.Equal(g, len(metadataEscape(byte(b), false)), n)
		})
	}

	// Cookie matcher escaping always produces regular expressions.
	assert.Equal(t, cookieMatcher.mode, alwaysEscape)
	assert.Panic(t, func() { cookieMatcherLen('*', noEscape) })
	assert.Panic(t, func() { cookieMatcherLen('*', dynamicEscape) })
	assert.Panic(t, func() { cookieMatcherEscape('*', false) })
	for b := 0; b < 0x100; b++ {
		assert.Group(fmt.Sprintf("cookie input: 0x%x", b), t, func(g *assert.G) {
			n, encoding := cookieMatcherLen(byte(b), alwaysEscape)
			assert.GreaterThanEqual(t, n, 1)
			assert.LessThanEqual(t, n, 8)

			switch n {
			case 1:
				assert.Equal(g, encoding, notEncoded)
			case 3:
				assert.Equal(g, encoding, percentEncoded)
			case 2, 7, 8:
				assert.Equal(g, encoding, regexEncoded)
			}

			assert.Equal(g, len(cookieMatcherEscape(byte(b), true)), n)
		})
	}

	// Header matcher escaping may produce regular expressions.
	assert.Equal(t, headerMatcher.mode, dynamicEscape)
	assert.Panic(t, func() { headerMatcherLen('*', noEscape) })
	assert.Panic(t, func() { headerMatcherEscape(';', false) })
	for b := 0; b < 0x100; b++ {
		assert.Group(fmt.Sprintf("header input, no previous regex: 0x%x", b), t, func(g *assert.G) {
			n, encoding := headerMatcherLen(byte(b), dynamicEscape)

			switch n {
			case 1:
				assert.Equal(g, encoding, notEncoded)
			case 3:
				assert.Equal(g, encoding, percentEncoded)
			case 7, 8:
				assert.Equal(g, encoding, regexEncoded)
			default:
				assert.Failed(g, fmt.Sprintf("unexpected headerMatcherLen: %d", n))
			}

			if encoding == regexEncoded {
				assert.Equal(g, len(headerMatcherEscape(byte(b), true)), n)
			} else {
				assert.Equal(g, len(headerMatcherEscape(byte(b), false)), n)
			}
		})

		assert.Group(
			fmt.Sprintf("header input, with previous regex: 0x%x", b),
			t,
			func(g *assert.G) {
				n, encoding := headerMatcherLen(byte(b), alwaysEscape)

				switch n {
				case 1:
					assert.Equal(g, encoding, notEncoded)
				case 3:
					assert.Equal(g, encoding, percentEncoded)
				case 2, 7, 8:
					assert.Equal(g, encoding, regexEncoded)
				default:
					assert.Failed(g, fmt.Sprintf("unexpected headerMatcherLen: %d", n))
				}

				assert.Equal(g, len(headerMatcherEscape(byte(b), true)), n)
			},
		)
	}

	// Query parameter escaping never produces regular expressions and
	// therefore does not allow them to be escaped.
	assert.Equal(t, queryMatcher.mode, noEscape)
	assert.Panic(t, func() { queryMatcherLen('*', dynamicEscape) })
	assert.Panic(t, func() { queryMatcherLen('*', alwaysEscape) })
	assert.Panic(t, func() { queryMatcherEscape('*', true) })
	for b := 0; b < 0x100; b++ {
		assert.Group(fmt.Sprintf("query parameter input: 0x%x", b), t, func(g *assert.G) {
			n, encoding := queryMatcherLen(byte(b), noEscape)
			assert.GreaterThanEqual(g, n, 1)
			assert.LessThanEqual(g, n, 3)
			assert.NotEqual(g, n, 2)
			assert.NotEqual(g, encoding, regexEncoded)

			assert.Equal(g, len(queryMatcherEscape(byte(b), false)), n)
		})
	}
}

func TestEscapeMetadata(t *testing.T) {
	testCases := [][]string{
		{``, ``},
		{`simple`, `simple`},
		{` `, `%20`},
		{`{}`, `{}`},
		{`"`, `%22`},
		{`%`, `%25`},
		{`%%`, `%25%25`},
		{`%1F`, `%251F`},
		{`,`, `%2c`},
		{`;`, `%3b`},
		{`;;`, `%3b%3b`},
		{`\`, `%5c`},
		{`~`, `%7e`},
		{`*`, `*`},
		{`user.2018-01-31.user-branch.abcdef12`, `user.2018-01-31.user-branch.abcdef12`},
	}

	for i, testCase := range testCases {
		assert.Group(fmt.Sprintf("input: %q (%d of %d)", testCase[0], i+1, len(testCases)),
			t,
			func(g *assert.G) {
				escaped := escapeMetadata(testCase[0])
				assert.Equal(g, escaped, testCase[1])
			},
		)
	}
}

func TestEscapeMetadataControlChars(t *testing.T) {
	bytes := []byte{}
	for i := 0; i < 0x20; i++ {
		bytes = append(bytes, byte(i))
	}
	for i := 0x7e; i < 0x100; i++ {
		bytes = append(bytes, byte(i))
	}

	for _, b := range bytes {
		assert.Group(fmt.Sprintf("input: 0x%02x", b),
			t,
			func(g *assert.G) {
				input := string([]byte{b})
				expected := fmt.Sprintf("%%%02x", b)
				escaped := escapeMetadata(input)
				assert.Equal(g, escaped, expected)
			},
		)
	}
}

func TestHeaderMatcherForMetadata(t *testing.T) {
	testCases := []struct {
		input           string
		expectedMatcher string
		expectedIsRegex bool
	}{
		{``, ``, false},
		{`simple`, `simple`, false},
		{"\n", `%0a`, false},
		{`{}`, `(%7b|\{)(%7d|\})`, true},
		{`{;}`, `(%7b|\{)(%3b|;)(%7d|\})`, true},
		{`[:space:]`, `(%5b|\[)(%3a|:)space(%3a|:)(%5d|\])`, true},
		{`x.-_0`, `x.-_0`, false},
		{
			`^%(x+|y.|z*)?$`,
			`(%5e|\^)(%25|%)(%28|\()x(%2b|\+)(%7c|\|)y\.(%7c|\|)z(%2a|\*)(%29|\))(%3f|\?)(%24|\$)`,
			true,
		},
		{` `, `(%20| )`, true},
		{`"`, `(%22|")`, true},
		{`%`, `(%25|%)`, true},
		{`%%`, `(%25|%)(%25|%)`, true},
		{`%1F`, `(%25|%)1F`, true},
		{`,`, `(%2c|,)`, true},
		{`;`, `(%3b|;)`, true},
		{`;;`, `(%3b|;)(%3b|;)`, true},
		{`\`, `(%5c|\\)`, true},
		{`~`, `(%7e|~)`, true},
		{`user.2018-01-31.user-branch.abcdef12`, `user.2018-01-31.user-branch.abcdef12`, false},
	}

	for i, testCase := range testCases {
		assert.Group(fmt.Sprintf("input: %q (%d of %d)", testCase.input, i+1, len(testCases)),
			t,
			func(g *assert.G) {
				matcher, isRegex := headerMatcherForMetadata(testCase.input)
				assert.Equal(g, matcher, testCase.expectedMatcher)
				assert.Equal(g, isRegex, testCase.expectedIsRegex)
			},
		)
	}
}

func TestCookieMatcherForMetadata(t *testing.T) {
	testCases := [][]string{
		{``, ``},
		{`simple`, `simple`},
		{` `, `%20`},
		{`{}`, `(%7b|\{)(%7d|\})`},
		{`[:space:]`, `(%5b|\[)(%3a|:)space(%3a|:)(%5d|\])`},
		{
			`^(x+|y.|z*)?$`,
			`(%5e|\^)(%28|\()x(%2b|\+)(%7c|\|)y\.(%7c|\|)z(%2a|\*)(%29|\))(%3f|\?)(%24|\$)`,
		},
		{`"`, `%22`},
		{`%`, `%25`},
		{`%%`, `%25%25`},
		{`%1F`, `%251F`},
		{`,`, `%2c`},
		{`;`, `%3b`},
		{`;;`, `%3b%3b`},
		{`\`, `%5c`},
		{`~`, `%7e`},
		{`*`, `(%2a|\*)`},
		{`user.2018-01-31.user-branch.abcdef12`, `user\.2018-01-31\.user-branch\.abcdef12`},
	}

	for i, testCase := range testCases {
		assert.Group(fmt.Sprintf("input: %q (%d of %d)", testCase[0], i+1, len(testCases)),
			t,
			func(g *assert.G) {
				matcher := cookieMatcherForMetadata(testCase[0])
				assert.Equal(g, matcher, testCase[1])
			},
		)
	}
}

func TestQueryMatcherForMetadata(t *testing.T) {
	safeRunes := []byte{}
	unsafeRunes := []byte{}
	escapedRunes := []byte{}
	for r := rune(0); r < 0x7f; r++ {
		b := byte(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			safeRunes = append(safeRunes, b)
		} else {
			unsafeRunes = append(unsafeRunes, b)
			escapedRunes = append(escapedRunes, []byte(fmt.Sprintf("%%%02x", b))...)
		}
	}

	testCases := [][]string{
		{``, ``},
		{string(unsafeRunes), string(escapedRunes)},
		{string(safeRunes), string(safeRunes)},
		{` `, `%20`},
		{`{}`, `%7b%7d`},
		{`[:space:]`, `%5b%3aspace%3a%5d`},
		{`"`, `%22`},
		{`%`, `%25`},
		{`%%`, `%25%25`},
		{`%1F`, `%251F`},
		{`user.2018-01-31.user-branch.abcdef12`, `user.2018-01-31.user-branch.abcdef12`},
	}

	for i, testCase := range testCases {
		assert.Group(fmt.Sprintf("input: %q (%d of %d)", testCase[0], i+1, len(testCases)),
			t,
			func(g *assert.G) {
				matcher := queryMatcherForMetadata(testCase[0])
				assert.Equal(g, matcher, testCase[1])
			},
		)
	}
}
