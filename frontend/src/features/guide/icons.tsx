import type { ReactNode } from 'react'
import {
  Code2,
  Download,
  Globe2,
  LayoutDashboard,
  Monitor,
  Send,
  Smartphone
} from 'lucide-react'

export const guideIcons: Record<string, ReactNode> = {
  install: <Download />,
  manage: <LayoutDashboard />,
  develop: <Code2 />,
  deploy: <Send />,
  desktop: <Monitor />,
  mobile: <Smartphone />,
  web: <Globe2 />
}
