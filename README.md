<p align="center">
  <img src="ui/public/favicon.svg" width="112" height="112" alt="WarmMo 屋墨 Logo" />
</p>

<h1 align="center">WarmMo · 屋墨</h1>

<p align="center">
  面向长篇故事创作的 AI 写作工作台，把灵感通过画布形式组织成可追踪、可推演、可持续的故事世界。
</p>

<p align="center">
  <a href="#线上地址">线上地址</a>
  ·
  <a href="#核心功能">核心功能</a>
  ·
  <a href="#工作方式">工作方式</a>
  ·
  <a href="#快速开始">快速开始</a>
  ·
  <a href="#路线图">路线图</a>
</p>

## 线上地址

> [WarmMo · 屋墨](https://zhiyanzhaijie.github.io/warmmo)

## 核心功能

- **故事画布**：在可缩放画布上创建、编辑、连接和排列角色、物品、地点、时间、世界观、机制、事件与章节概览。
- **结构化写作链路**：归档完成创作章节沉淀故事线。
- **上下文 Agent**：围绕整个作品或选定节点展开创作与讨论，实时展示回答、推理阶段和工具执行进度。
- **多 Agent 协作**：由编排 Agent 按任务调用规划、实体创作、章节创作、正文创作、故事脑暴与润色能力。
- **版本与归档**：保存节点修订历史，支持画布操作撤销与重做；章节归档拥有独立版本，可查看历史或撤回当前归档。
- **本地优先**：作品、节点、会话、运行记录、Checkpoint 与 Agent 产物由使用者本地 Go Core 写入 SQLite。

## 工作方式

WarmMo 由 Web UI 与本地 Core 两部分组成：

```text
浏览器中的 WarmMo UI
        │
        │ HTTP + 流式事件
        ▼
本地 WarmMo Core · 127.0.0.1:8787
        ├── SQLite 作品与运行数据
        ├── Google ADK Agent Runtime
        ├── 内置写作 Skills
        └── 用户配置的模型 Provider
```

浏览器负责画布交互、内容编辑与 Agent 会话；本地 Core 负责数据持久化、模型调用、上下文检索和写作任务编排。模型请求会发送到你所配置的 Provider。

## 快速开始

### 使用 Release

1. 打开 [Releases](https://github.com/zhiyanzhaijie/warmmo/releases/latest)，下载与系统架构对应的 WarmMo Core。
2. 解压后运行包根目录中的启动脚本：
   - macOS：`start-warmmo-core.command`
   - Windows：`start-warmmo-core.cmd`
   - Linux：`start-warmmo-core.sh`
3. 打开 WarmMo Web UI，在“设置”中添加文本模型 Provider。
4. 返回首页，描述你的故事想法并开始创建作品。

> 各平台由于缺乏公证，首次运行被安全机制拦截，请参考web界面引导放行：

## 路线图

- [x] 故事节点无限画布与关系编辑
- [x] 节点级 AI 修改与结构化派生
- [x] 全局创作 Agent、上下文选择与流式会话
- [ ] 完稿组织、导出与发布工作流
- [ ] 更完整的搜索、统计与长篇一致性检查
- [ ] 更多可组合的创作 Skills

## 许可证

WarmMo 基于 [MIT License](LICENSE) 开源。你可以自由使用、复制、修改、合并、发布、分发、再许可和销售本项目，包括基于本项目进行二次开发与商业化；使用时需保留原始版权声明与许可证文本。
