# Code Review Skill（工单20）

在 Cursor/Trae 中启用本技能：

1. 将 `cursor-skills/code-review/` 复制或软链到项目的 `.cursor/skills/code-review/`（Trae 为 `.trae/skills/`）。
2. 重启 IDE，输入 `/review`（审查未提交变更）或 `/review internal/service`（审查目录）。
3. 也可自然语言触发：“帮我代码审查 internal/handler”。

技能定义见 `SKILL.md`：四维审查（正确性/安全/性能/可维护性），输出 Markdown 结构化报告，
判级分为阻断/严重/建议三级。`--strict` 模式额外检查工单编号注释完整性。
