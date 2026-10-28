import type { HttpInterceptorFn } from "@angular/common/http";
import type { Provider } from "@angular/core";
import { mockInterceptor } from "../app/mock/mock-interceptor";
import { mockSocketFactoryProvider } from "../app/mock/mock-socket";
import type { Environment } from "./environment-type";

export const environment: Environment = { mock: true, apiBase: "/api/v1" };

export const backendInterceptors: readonly HttpInterceptorFn[] = [mockInterceptor];

export const backendProviders: readonly Provider[] = [mockSocketFactoryProvider];
