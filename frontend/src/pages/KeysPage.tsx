import { Navigate } from 'react-router-dom';

export default function KeysPage() {
  return <Navigate to="/applications?tab=legacy" replace />;
}
