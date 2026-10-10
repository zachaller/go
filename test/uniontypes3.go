// compiledir -goexperiment uniontypes

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test that union (sum) types work across packages, including
// export data, inlining, and generic instantiation.

package ignored
