// Package usecase orchestrates application logic.
//
// Each CLI verb has a single exported function in its own file
// (e.g. Claim in claim.go). Functions take a context, a Deps struct
// of domain interfaces, and an input struct; they return an output
// struct (or just an error). They never import adapters directly.
package usecase
