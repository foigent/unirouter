import {
  MemoryRouter as ReactRouterMemoryRouter,
  type MemoryRouterProps,
} from 'react-router-dom';
import { ROUTER_FUTURE_FLAGS } from '../routing/routerFuture';

export function MemoryRouter(props: Omit<MemoryRouterProps, 'future'>) {
  return <ReactRouterMemoryRouter {...props} future={ROUTER_FUTURE_FLAGS} />;
}
