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

// Package networking contains interfaces to the database models for Network ACLs and Routes.
package networking

import (
	"sort"
	"strings"

	v1 "github.com/webmeshproj/api/v1"

	"github.com/webmeshproj/node/pkg/context"
	"github.com/webmeshproj/node/pkg/meshdb/rbac"
	"github.com/webmeshproj/node/pkg/storage"
)

// ACLs is a list of Network ACLs. It contains methods for evaluating actions against
// contained permissions. It also allows for sorting by priority.
type ACLs []*ACL

// Proto returns the protobuf representation of the ACLs.
func (a ACLs) Proto() []*v1.NetworkACL {
	if a == nil {
		return nil
	}
	proto := make([]*v1.NetworkACL, len(a))
	for i, acl := range a {
		proto[i] = acl.Proto()
	}
	return proto
}

// ACL is a Network ACL. It contains a reference to the database for evaluating group membership.
type ACL struct {
	*v1.NetworkACL
	storage storage.Storage
}

// Proto returns the protobuf representation of the ACL.
func (a *ACL) Proto() *v1.NetworkACL {
	return a.NetworkACL
}

// SortDirection is the direction to sort ACLs.
type SortDirection int

const (
	// SortDescending sorts ACLs in descending order.
	SortDescending SortDirection = iota
	// SortAscending sorts ACLs in ascending order.
	SortAscending
)

// Len returns the length of the ACLs list.
func (a ACLs) Len() int { return len(a) }

// Swap swaps the ACLs at the given indices.
func (a ACLs) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less returns whether the ACL at index i should be sorted before the ACL at index j.
func (a ACLs) Less(i, j int) bool {
	return a[i].Priority < a[j].Priority
}

// Sort sorts the ACLs by priority.
func (a ACLs) Sort(direction SortDirection) {
	switch direction {
	case SortAscending:
		sort.Sort(a)
	case SortDescending:
		sort.Sort(sort.Reverse(a))
	default:
		sort.Sort(sort.Reverse(a))
	}
}

// Accept evaluates an action against the ACLs in the list. It assumes the ACLs
// are sorted by priority. The first ACL that matches the action will be used.
// If no ACL matches, the action is denied.
func (a ACLs) Accept(ctx context.Context, action *v1.NetworkAction) bool {
	if a == nil {
		return false
	}
	for _, acl := range a {
		if acl.Matches(ctx, action) {
			return acl.Action == v1.ACLAction_ACTION_ACCEPT
		}
	}
	return false
}

// Matches checks if an action matches this ACL. If a database query fails it will log the
// error and return false.
func (acl *ACL) Matches(ctx context.Context, action *v1.NetworkAction) bool {
	if action.GetSrcNode() != "" {
		if len(acl.GetSourceNodes()) >= 0 {
			// Are we expanding any groups?
			groups := make(map[string][]string)
			for _, node := range acl.GetSourceNodes() {
				if strings.HasPrefix(node, "group:") {
					if _, ok := groups[node]; ok {
						continue
					}
					groupName := strings.TrimPrefix(node, "group:")
					group, err := rbac.New(acl.storage).GetGroup(ctx, groupName)
					if err != nil {
						if err != rbac.ErrGroupNotFound {
							context.LoggerFrom(ctx).Error("failed to get group", "group", groupName, "error", err)
							return false
						}
						// If the group doesn't exist, we'll just ignore it.
						continue
					}
					for _, subject := range group.GetSubjects() {
						if subject.GetType() == v1.SubjectType_SUBJECT_ALL || subject.GetType() == v1.SubjectType_SUBJECT_NODE {
							groups[groupName] = append(groups[groupName], subject.GetName())
						}
					}
				}
			}
			// Replace group references with their members.
			for groupName, members := range groups {
			SrcNodes:
				for _, node := range acl.GetSourceNodes() {
					if node == "group:"+groupName {
						acl.SourceNodes = replace(acl.SourceNodes, node, members)
						break SrcNodes
					}
				}
			}
			if !containsOrWildcardMatch(acl.GetSourceNodes(), action.GetSrcNode()) {
				return false
			}
		}
	}
	if action.GetDstNode() != "" {
		if len(acl.GetDestinationNodes()) >= 0 {
			// Are we expanding any groups?
			groups := make(map[string][]string)
			for _, node := range acl.GetDestinationNodes() {
				if strings.HasPrefix(node, "group:") {
					if _, ok := groups[node]; ok {
						continue
					}
					groupName := strings.TrimPrefix(node, "group:")
					group, err := rbac.New(acl.storage).GetGroup(ctx, groupName)
					if err != nil {
						if err != rbac.ErrGroupNotFound {
							context.LoggerFrom(ctx).Error("failed to get group", "group", groupName, "error", err)
							return false
						}
						// If the group doesn't exist, we'll just ignore it.
						continue
					}
					for _, subject := range group.GetSubjects() {
						if subject.GetType() == v1.SubjectType_SUBJECT_ALL || subject.GetType() == v1.SubjectType_SUBJECT_NODE {
							groups[groupName] = append(groups[groupName], subject.GetName())
						}
					}
				}
			}
			// Replace group references with their members.
			for groupName, members := range groups {
			DstNodes:
				for _, node := range acl.GetDestinationNodes() {
					if node == "group:"+groupName {
						acl.DestinationNodes = replace(acl.DestinationNodes, node, members)
						break DstNodes
					}
				}
			}
			if !containsOrWildcardMatch(acl.GetDestinationNodes(), action.GetDstNode()) {
				return false
			}
		}
	}
	if action.GetSrcCidr() != "" {
		if len(acl.GetSourceCidrs()) >= 0 {
			if !containsOrWildcardMatch(acl.GetSourceCidrs(), action.GetSrcCidr()) {
				return false
			}
		}
	}
	if action.GetDstCidr() != "" {
		if len(acl.GetDestinationCidrs()) >= 0 {
			if !containsOrWildcardMatch(acl.GetDestinationCidrs(), action.GetDstCidr()) {
				return false
			}
		}
	}
	if action.GetProtocol() != "" {
		if len(acl.GetProtocols()) >= 0 {
			if !containsOrWildcardMatch(acl.GetProtocols(), action.GetProtocol()) {
				return false
			}
		}
	}
	if action.GetPort() != 0 {
		if len(acl.GetPorts()) >= 0 {
			if !containsPort(acl.GetPorts(), action.GetPort()) {
				return false
			}
		}
	}
	return true
}

func containsOrWildcardMatch(ss []string, s string) bool {
	for _, v := range ss {
		if v == "*" {
			return true
		} else if strings.Contains(v, "*") {
			if strings.HasPrefix(v, "*") {
				if strings.HasSuffix(s, strings.TrimPrefix(v, "*")) {
					return true
				}
			} else if strings.HasSuffix(v, "*") {
				if strings.HasPrefix(s, strings.TrimSuffix(v, "*")) {
					return true
				}
			} else {
				parts := strings.Split(v, "*")
				if strings.HasPrefix(s, parts[0]) && strings.HasSuffix(s, parts[1]) {
					return true
				}
			}
		} else if v == s {
			return true
		}
	}
	return false
}

func containsPort(pp []uint32, p uint32) bool {
	for _, v := range pp {
		if v == p {
			return true
		}
	}
	return false
}

func replace(in []string, obj string, with []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == obj {
			out = append(out, with...)
		} else {
			out = append(out, v)
		}
	}
	return out
}
