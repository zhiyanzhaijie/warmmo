import { createBrowserRouter, RouterProvider } from 'react-router-dom'

import { AppLayout } from './layouts/AppLayout'
import { CanvasLayout } from './layouts/CanvasLayout'
import { CanvasPage } from './pages/CanvasPage'
import { HomePage } from './pages/HomePage'
import { SettingsPage } from './pages/SettingsPage'
import { WorkspacePage } from './pages/WorkspacePage'

const router = createBrowserRouter([
  {
    element: <AppLayout />,
    children: [
      { index: true, element: <HomePage /> },
      { path: 'workspace', element: <WorkspacePage /> },
      { path: 'settings', element: <SettingsPage /> },
    ],
  },
  {
    element: <CanvasLayout />,
    children: [
      { path: 'works/:workId', element: <CanvasPage /> },
    ],
  },
])

export function App() {
  return <RouterProvider router={router} />
}
