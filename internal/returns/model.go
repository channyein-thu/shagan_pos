package returns

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Return struct {
	gorm.Model
}

type ReturnItem struct {
	gorm.Model
}

type Exchange struct {
	gorm.Model
}

type ExchangeItem struct {
	gorm.Model
}

type Void struct {
	gorm.Model
}
