package route

import "context"

func Get[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "GET"
	Register(r, op, handler)
}
func Post[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "POST"
	Register(r, op, handler)
}
func Put[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "PUT"
	Register(r, op, handler)
}
func Patch[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "PATCH"
	Register(r, op, handler)
}
func Delete[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "DELETE"
	Register(r, op, handler)
}
func Head[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "HEAD"
	Register(r, op, handler)
}
func Options[B, Q, P, R any](r *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = "OPTIONS"
	Register(r, op, handler)
}
