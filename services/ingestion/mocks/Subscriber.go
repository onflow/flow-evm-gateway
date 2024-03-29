// Code generated by mockery v2.21.4. DO NOT EDIT.

package mocks

import (
	context "context"

	flow "github.com/onflow/flow-go-sdk"

	mock "github.com/stretchr/testify/mock"
)

// Subscriber is an autogenerated mock type for the Subscriber type
type Subscriber struct {
	mock.Mock
}

// Subscribe provides a mock function with given fields: ctx, height
func (_m *Subscriber) Subscribe(ctx context.Context, height uint64) (<-chan flow.BlockEvents, <-chan error, error) {
	ret := _m.Called(ctx, height)

	var r0 <-chan flow.BlockEvents
	var r1 <-chan error
	var r2 error
	if rf, ok := ret.Get(0).(func(context.Context, uint64) (<-chan flow.BlockEvents, <-chan error, error)); ok {
		return rf(ctx, height)
	}
	if rf, ok := ret.Get(0).(func(context.Context, uint64) <-chan flow.BlockEvents); ok {
		r0 = rf(ctx, height)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(<-chan flow.BlockEvents)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, uint64) <-chan error); ok {
		r1 = rf(ctx, height)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(<-chan error)
		}
	}

	if rf, ok := ret.Get(2).(func(context.Context, uint64) error); ok {
		r2 = rf(ctx, height)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

type mockConstructorTestingTNewSubscriber interface {
	mock.TestingT
	Cleanup(func())
}

// NewSubscriber creates a new instance of Subscriber. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewSubscriber(t mockConstructorTestingTNewSubscriber) *Subscriber {
	mock := &Subscriber{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
