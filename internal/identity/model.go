package identity

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Organization struct {
	gorm.Model
}

type Branch struct {
	gorm.Model
}

type User struct {
	gorm.Model
}

type Staff struct {
	gorm.Model
}

type Role struct {
	gorm.Model
}

type Permission struct {
	gorm.Model
}

type RolePermission struct {
	gorm.Model
}

type Session struct {
	gorm.Model
}

type Device struct {
	gorm.Model
}
