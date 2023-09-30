/*
Copyright 2023 Avi Zimmerman <avi.zimmerman@gmail.com>

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

package peers

import (
	"testing"

	"github.com/webmeshproj/webmesh/pkg/storage"
	"github.com/webmeshproj/webmesh/pkg/storage/providers/backends/badgerdb"
	"github.com/webmeshproj/webmesh/pkg/storage/testutil"
)

func TestPeers(t *testing.T) {
	t.Parallel()
	testutil.TestPeerStorageConformance(t, func(t *testing.T) storage.Peers {
		st := badgerdb.NewTestDiskStorage(false)
		p := New(st)
		t.Cleanup(func() {
			_ = st.Close()
		})
		return p
	})
}