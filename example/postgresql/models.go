package main

// User represents a user in the database
type User struct {
	Name   string `db:"name"`
	Email  string `db:"email"`
	Age    int    `db:"age"`
	Status string `db:"status"`
}

// TableName returns the table name for the User struct
func (u *User) TableName() string {
	return "users"
}

// Product represents a product in the database
type Product struct {
	Name        string  `db:"name"`
	Description string  `db:"description"`
	Price       float64 `db:"price"`
	Stock       int     `db:"stock"`
	Category    string  `db:"category"`
}

// TableName returns the table name for the Product struct
func (p *Product) TableName() string {
	return "products"
}

// Order represents an order in the database
type Order struct {
	UserID     int     `db:"user_id"`
	ProductID  int     `db:"product_id"`
	Quantity   int     `db:"quantity"`
	TotalPrice float64 `db:"total_price"`
	Status     string  `db:"status"`
}

// TableName returns the table name for the Order struct
func (o *Order) TableName() string {
	return "orders"
}
