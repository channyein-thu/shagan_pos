package catalog

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Product struct {
	gorm.Model
}

type Category struct {
	gorm.Model
}

type Combo struct {
	gorm.Model
}

type ComboItem struct {
	gorm.Model
}

type ProductImage struct {
	gorm.Model
}
