package security

import (
	"sort"
)

func NewRoleHierarchy(inheritedRolesByRole map[string][]string) *RoleHierarchy {
	if nil == inheritedRolesByRole {
		inheritedRolesByRole = map[string][]string{}
	}

	return &RoleHierarchy{
		inheritedRolesByRole: deepCopyRoleHierarchy(inheritedRolesByRole),
	}
}

type RoleHierarchy struct {
	inheritedRolesByRole map[string][]string
}

func (instance *RoleHierarchy) ExpandRoles(roles []string) []string {
	expanded := map[string]bool{}
	queue := make([]string, 0)

	for _, role := range roles {
		if "" == role {
			continue
		}

		if true == expanded[role] {
			continue
		}

		expanded[role] = true
		queue = append(queue, role)
	}

	for 0 < len(queue) {
		current := queue[0]
		queue = queue[1:]

		inheritedRoles, exists := instance.inheritedRolesByRole[current]
		if false == exists {
			continue
		}

		for _, inheritedRole := range inheritedRoles {
			if "" == inheritedRole {
				continue
			}

			if true == expanded[inheritedRole] {
				continue
			}

			expanded[inheritedRole] = true
			queue = append(queue, inheritedRole)
		}
	}

	result := make([]string, 0, len(expanded))
	for role := range expanded {
		result = append(result, role)
	}

	sort.Strings(result)

	return result
}

func deepCopyRoleHierarchy(inheritedRolesByRole map[string][]string) map[string][]string {
	if nil == inheritedRolesByRole {
		return map[string][]string{}
	}

	copied := make(map[string][]string, len(inheritedRolesByRole))

	for role, inheritedRoleList := range inheritedRolesByRole {
		copied[role] = append([]string{}, inheritedRoleList...)
	}

	return copied
}
