# Package subtest

Go 1.7 introduced subtests and sub-benchmarks. This package allows this
functionality to be used in a somewhat backwards compatible way.

Using subtest.Run wil get you all the features of subtests in Go 1.7.
Subtests pre-Go 1.7 will not be great, but at least all tests will be
run.

# Example

```go
package foo

import "github.com/mpvl/subtest"

var testCases = ...

func TestFoo(t *testing.T) {
	for _, tc := range testCases {
		subtest.Run(t, tc.name, func(t *testing.T) {
			tc.doTest()
		})
	}
}
```
