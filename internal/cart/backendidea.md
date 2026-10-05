A browser or frontend sends an HTTP request
The router receives it
A handler reads the request
The service decides the business rule
The repository talks to MySQL
MySQL stores or reads the data
The response comes back


The main layers
1) Database layer
This is MySQL. It stores the actual data.

The SQL schema lives in:

0003_cart_schema.up.sql
This file creates the tables for:

cart
cart_item
It is like the “storage room” for your app.

2) Domain model
This is the shape of the data.

File:

cart.go
Inside it, you define objects like:

Cart
CartItem
Money
This is not the database itself. It is just the Go representation of the cart data.

Example:

one cart belongs to one customer
one cart can have many cart items
each cart item has quantity and variant ID
3) Repository layer
This is where SQL lives.

File:

cart_repository.go
The repository is responsible for:

insert data
fetch data
update quantity
delete item
join tables
Examples:

Get loads the cart
UpsertLine adds or updates an item
FindLine finds one item
DeleteLine removes one item
This layer knows how to talk to MySQL.

4) Service layer
This is business logic.

File:

service.go
The service checks rules like:

is the customer ID valid?
is quantity at least 1?
does the item exist?
should it update or add?
This is the place for rules, not for SQL.

Example:

AddItem validates input
UpdateItemQuantity checks item exists
RemoveItem removes item and reloads cart
So:

repository = database work
service = business rules
5) HTTP layer
This is the API layer that receives requests from the frontend.

The app folder is:

httpapi
This layer:

reads JSON from the request
calls the service
sends JSON responses
This is where your endpoints live, for example:

GET /cart
POST /cart/items
PATCH /cart/items/:id
6) Main router
This is the entry point.

File:

main.go
This file:

loads config
opens database connection
creates the router
registers routes
starts the server
This is the “composition root” — the place where all pieces are wired together.

Request flow example
When someone adds a product to cart:

Frontend calls POST /cart/items
Router matches the URL
Handler reads JSON like:
{ "variant_id": 12, "quantity": 2 }
Handler calls service.AddItem(...)
Service validates values
Service calls repository UpsertLine(...)
Repository inserts/updates in MySQL
Repository gets updated cart from Get(...)
Response returns the cart JSON
So the flow is:

HTTP request → handler → service → repository → database → response

Very short version
MySQL = storage
Repository = SQL
Service = rules
Handler = API
Main = wiring