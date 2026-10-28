import type { HttpInterceptorFn } from "@angular/common/http";
import type { Provider } from "@angular/core";
import type { Environment } from "./environment-type";

export const environment: Environment = { mock: false, apiBase: "/api/v1" };

/** Interceptors that stand in for the backend; empty outside mock mode. */
export const backendInterceptors: readonly HttpInterceptorFn[] = [];

/** Providers that stand in for the backend; empty outside mock mode. */
export const backendProviders: readonly Provider[] = [];
