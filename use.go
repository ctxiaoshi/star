package star

// Component 组件类型
type Component interface {
	Middleware | []Middleware | Route | []Route | BeforeRouteGuard | []BeforeRouteGuard | AfterRouteGuard | []AfterRouteGuard
}

// Use 应用Star组件
func Use[T Component](component T) {
	panicIfStarInstanceIsNotInitialized()

	switch v := any(component).(type) {
	case Middleware:
		starInstance.router.useMiddleware([]Middleware{v})
	case []Middleware:
		starInstance.router.useMiddleware(v)
	case Route:
		starInstance.router.useRoutes([]Route{v})
	case []Route:
		starInstance.router.useRoutes(v)
	case BeforeRouteGuard:
		starInstance.router.useBeforeHandler(v)
	case AfterRouteGuard:
		starInstance.router.useAfterHandler(v)
	case []BeforeRouteGuard:
		starInstance.router.useBeforeHandler(v...)
	case []AfterRouteGuard:
		starInstance.router.useAfterHandler(v...)
	}
}
