package model

type Permission int64

const (
	PermViewChannels    Permission = 1 << 0
	PermManageChannels  Permission = 1 << 1
	PermManageRoles     Permission = 1 << 2
	PermManageServer    Permission = 1 << 3
	PermCreateInvite    Permission = 1 << 4
	PermKickMembers     Permission = 1 << 5
	PermBanMembers      Permission = 1 << 6
	PermSendMessages    Permission = 1 << 7
	PermManageMessages  Permission = 1 << 8
	PermConnect         Permission = 1 << 9
	PermSpeak           Permission = 1 << 10
	PermMuteMembers     Permission = 1 << 11
	PermDeafenMembers   Permission = 1 << 12
	PermMoveMembers     Permission = 1 << 13
	PermUseWhisper      Permission = 1 << 14
	PermShareScreen     Permission = 1 << 15
	PermAttachFiles     Permission = 1 << 16
	PermAdministrator   Permission = 1 << 31
)

func HasPermission(perms, perm Permission) bool {
	if perms&PermAdministrator != 0 {
		return true
	}
	return perms&perm != 0
}

func ComputePermissions(basePerms []int64, overrideAllow, overrideDeny int64) Permission {
	var combined int64
	for _, p := range basePerms {
		combined |= p
	}
	combined = (combined &^ overrideDeny) | overrideAllow
	return Permission(combined)
}
