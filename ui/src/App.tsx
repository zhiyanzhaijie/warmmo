import { useEffect } from 'react'
import { createBrowserRouter, Outlet, RouterProvider, useMatches } from 'react-router-dom'

import { AppLayout } from './layouts/AppLayout'
import { CanvasLayout } from './layouts/CanvasLayout'
import { CanvasPage } from './pages/CanvasPage'
import { HomePage } from './pages/HomePage'
import { NodeEditorPage } from './pages/NodeEditorPage'
import { RuntimePage } from './pages/RuntimePage'
import { SettingsPage } from './pages/SettingsPage'
import { WorkspacePage } from './pages/WorkspacePage'

type PageMetadata = {
  title: string
  description: string
  keywords: string
}

const defaultMetadata: PageMetadata = {
  title: '屋墨',
  description: 'WarmMo 是面向长篇创作的灵感组织与写作工作台。',
  keywords: 'WarmMo,写作,小说创作,灵感管理',
}

const router = createBrowserRouter([
  {
    element: <RouteMetadata />,
    children: [
      {
        element: <AppLayout />,
        children: [
          { index: true, element: <HomePage />, handle: { metadata: defaultMetadata } },
          {
            path: 'workspace',
            element: <WorkspacePage />,
            handle: { metadata: { title: '工作空间', description: '管理你的 WarmMo 创作项目与故事世界。', keywords: 'WarmMo,作品管理,小说项目' } },
          },
          {
            path: 'settings',
            element: <SettingsPage />,
            handle: { metadata: { title: '设置', description: '配置 WarmMo 的模型服务与应用偏好。', keywords: 'WarmMo,应用设置,模型配置' } },
          },
          {
            path: 'runtime',
            element: <RuntimePage />,
            handle: { metadata: { title: '运行状态', description: '查看 WarmMo 本地核心服务的连接与运行状态。', keywords: 'WarmMo,运行状态,本地服务' } },
          },
        ],
      },
      {
        element: <CanvasLayout />,
        children: [
          {
            path: 'works/:workId',
            element: <CanvasPage />,
            handle: { metadata: { title: '创作画布', description: '在 WarmMo 画布中组织故事节点、关系与创作思路。', keywords: 'WarmMo,创作画布,故事节点' } },
          },
        ],
      },
      {
        path: 'works/:workId/nodes/:nodeId/edit',
        element: <NodeEditorPage />,
        handle: { metadata: { title: '编辑节点', description: '编辑 WarmMo 创作画布中的故事节点内容。', keywords: 'WarmMo,节点编辑,内容创作' } },
      },
    ],
  },
], {
  basename: import.meta.env.PROD ? '/warmmo' : '/',
})

function RouteMetadata() {
  const matches = useMatches()
  const metadata = matches.reduce<PageMetadata>((current, match) => {
    const handle = match.handle as { metadata?: PageMetadata } | undefined
    return handle?.metadata ?? current
  }, defaultMetadata)

  useEffect(() => {
    document.title = `WarmMo · ${metadata.title}`
    setMetaContent('description', metadata.description)
    setMetaContent('keywords', metadata.keywords)
  }, [metadata])

  return <Outlet />
}

function setMetaContent(name: string, content: string) {
  let element = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
  if (!element) {
    element = document.createElement('meta')
    element.name = name
    document.head.append(element)
  }
  element.content = content
}

export function App() {
  return <RouterProvider router={router} />
}
