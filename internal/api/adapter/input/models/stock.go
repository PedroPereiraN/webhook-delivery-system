package model

type UpdateStockQuantity struct {
	Quantity int32           `json:"quantity" binding:"required,gt=0"`
	Type     AddOrRemoveType `json:"type" binding:"oneof=0 1"`
}

type AddOrRemoveType int

const (
	Add AddOrRemoveType = iota
	Remove
)
