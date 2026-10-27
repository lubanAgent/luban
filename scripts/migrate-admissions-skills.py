#!/usr/bin/env python3
"""Reproduce Skill documents from the pinned, verified prompt archive.

Run with --source <master-4c3f7b43 directory>. Generated documents and their
change manifest are committed; this script does not read databases or secrets.
"""

import argparse
import hashlib
import json
from pathlib import Path


SOURCES = {
    "requirementClarify": ("main", "理解运营需求，保留完整规则和来源，最多追问两个必要问题。"),
    "formAndEligibility": ("s1", "制作留资字段、真实选项和准入资格条件。"),
    "scoreCalculation": ("s2", "制作成绩字段、完整算分公式和多维组合覆盖。"),
    "schoolRecommendation": ("school_picker", "制作真实学校推荐规则，理解分数和排名方向、固定名单与补缺。"),
    "resultDisplay": ("s3", "制作决策树、推荐路径和按原名顺序展示的板块。"),
}

MANUAL_SKILLS = {
    "strategyCheck": "Main/S1/S2/S3/SchoolPicker 检查章节及本轮 Agent 一致性契约",
}

PREAMBLE = """## 单 Agent 执行协议

这是一个 Agent 的领域知识，不是独立子 Agent。旧称 Main/S1/S2/S3/SchoolPicker
分别表示需求、form、scoring、recommendation/display、学校计算知识。没有这些子模型。

- 服务端 taskKind 决定权限：chat 只编辑 requirements；generate 只编辑 spec。
- queryBusinessData 查询当前任务资料；editStrategyDraft 以 expectedRevision
  完整替换恰好一个 requirements/form/scoring/recommendation/display 部分。
- 旧 JSON 例子展示领域结构，不是最终回复协议。将 intake/eligibility 放入 form；
  scoreInput/scoreCalc 放入 scoring；decisionTree 和 pipelineInput.paths 分别放入
  recommendation.decisionTree / recommendation.paths。display.sections 按展示顺序提供。
- 通过 checkStrategyDraft 检查并修正。业务 issues 是可修复结果；不要伪造检查成功。
- 最终只用 __runtime_output 提交 outcome/reply/draftRevision/checkId。
  chat: reply/readyToConfirm；generate: generated/needsInput。不能自行确认或发布。
- 已知但未验证的消费者行为（小数权重/平均精度、跨字段 validation、分组成绩表单）
  返回 unsupported_rule，不猜默认值，不删去用户规则后冒充成功。
- 文中示例不是当前城市政策；所有学校、字段和选项必须来自本任务快照。

"""


def migrate(stem, text):
    changes = []

    def replace(old, new, reason):
        nonlocal text
        count = text.count(old)
        if not count:
            raise ValueError(f"{stem}: missing expected source fragment: {old[:70]}")
        text = text.replace(old, new)
        changes.append({"kind": "replace", "reason": reason, "count": count, "old": old, "new": new})

    def section(start, end, replacement, reason):
        nonlocal text
        left = text.index(start)
        right = text.index(end, left) if end else len(text)
        old = text[left:right]
        text = text[:left] + replacement + "\n\n" + text[right:]
        changes.append({"kind": "section", "reason": reason, "old": old, "new": replacement})

    if stem == "main":
        section("## 9. 工具", "## 10.", """## 9. 需求编辑与结论

用 editStrategyDraft.requirements 完整替换三段需求，未修改的内容也必须保留。
包括 stage1Brief/stage2Brief/stage3Brief、openQuestions、explicitRules（text +
sourceMessageSeq）和 coveredMessageSeq。旧单段 update_brief 不再存在。
信息不足时提交 reply，并在 reply 中最多追问两个必要问题；信息齐全时先检查，
再提交 readyToConfirm 和有效 checkId，不能替人点击确认。""", "P16: three old tools replaced by complete requirements editing and OutputTool")
        section("## 13. 跨 Agent 契约段", None, """## 13. 同一草稿的部分契约

stage1Brief 对应 form 的 intake/eligibility；stage2Brief 对应 scoring 的
scoreInput/scoreCalc；stage3Brief 对应 recommendation 与 display。
需求卡片保持自然语言，不把 JSON/DSL 细节混入用户的完整 brief。
同一 Agent 在 generate 任务中按领域知识编辑各部分；不存在下游子 Agent。""", "replace obsolete multi-agent execution contract")
        replace("emit_summary", "检查后提交 readyToConfirm", "replace old finalization tool")
        replace("update_brief", "完整替换 requirements", "replace old partial edit tool")
        replace("下游 S3 / Validator 解决", "通过真实目录和当前草稿检查解决", "remove nonexistent downstream executor")
        replace("diff = schoolMetric - userMetric", "diff：分数为 schoolScore - userScore，排名为 userRank - schoolRank", "P15: distinguish score and ranking direction")
        replace("# Main · 需求翻译 Agent", "# 需求沟通与完整需求卡片", "single Agent domain title")
        replace("把运营的口语化需求翻译成下游 Agent 能消费的清晰 brief", "把运营的口语化需求翻译成同一草稿可消费的完整清晰 brief", "remove downstream model identity")
    elif stem == "s1":
        replace("- 你不与任何人对话;只输出一个 JSON 对象", "- 生成阶段通过 editStrategyDraft.form 提交完整 form，不另写最终裸 JSON", "replace standalone JSON protocol")
        replace("## 3. 输出契约 — 直接 JSON,无工具调用", "## 3. form 领域结构（通过 editStrategyDraft 提交）", "replace no-tool protocol")
        replace("`childrenName`", "`student_name`", "P01: consistent strategy field; legacy submission mapping remains a consumer gate")
        section("## 11. 失败处理", None, """## 11. 失败与修复

字段类型、未知引用、命名冲突和不支持的条件由 checkStrategyDraft 返回 issues。
模型按当前 revision 修改 form 后再次检查；没有 Wrapper 自动修复、固定两轮修复
或旧 Subtask。超限与基础设施错误按当前 Agent/业务任务规则失败。""", "replace nonexistent wrapper and fixed subtask retry")
        replace("# S1 · 留资 + 资格校验 配置产出器", "# 留资与资格配置", "single Agent domain title")
    elif stem == "s2":
        section("### 3.2 不输出其他任何结构", "## 4.", """### 3.2 当前提交方式

用 editStrategyDraft.scoring 提交完整 scoreInput/scoreCalc。
无法表达时保留明确缺口；不得删除规则或用零值假装完成。""", "replace unconditional standalone JSON response")
        replace("未知侧用 0 或 999 中最合理的", "未知侧列为必要缺口并询问运营，不能猜 0/999", "P03: do not invent bounds")
        replace("brief 完全没提范围 → min=0, max=999", "brief 完全没提范围 → 保留缺口，补齐明确范围后才能检查通过", "P03: do not invent bounds")
        start = text.index("### 示例 A")
        end = text.index("### 示例 B", start)
        fragment = text[start:end]
        old = '"validation": [\n      {\n        "type": "toast",\n        "condition": {"op": "gte", "field": "m1", "value": "m3"},\n        "message": "m1 不能大于 m3"\n      }\n    ]'
        if old not in fragment:
            raise ValueError("P07 source example changed")
        replace(old, '"validation": []', "P07: remove cross-field rule not requested in the example")
        section("## 12. 失败处理", None, """## 12. 当前检查与修复

checkStrategyDraft 校验公式、完整分支组合、字段引用及可执行边界。
修改后旧 checkId 作废。没有 Wrapper 去围栏、固定两轮重试或 S2 子任务。
平均、近似 0.33、小数 lookup 和跨字段 validation 未经学生页验证不能通过；
完整例子仍保留为领域知识，遇到不支持的形态明确报告，不改成错误的整数公式。""", "replace wrapper; P04/P05 consumer verification gate")
        replace("# S2 · 成绩输入 + 算分公式 配置产出器", "# 成绩输入与算分公式", "single Agent domain title")
    elif stem == "s3":
        replace("**硬约束**：整条回复是一个合法 JSON 对象。不要 markdown 围栏、不要解释文字、不要 tool_call。", "下列为领域结构示例。实际通过 editStrategyDraft.recommendation 提交 decisionTree/paths；不提交裸 JSON，不调用旧 S3 子模型。", "replace standalone JSON/no-tool protocol")
        section("## 15. 跨 Agent 契约", "## 16.", """## 15. 同一草稿与确定性计算

form 的真实字段和 scoring 的完整公式是 decisionTree 的引用来源。
旧 pipelineInput.paths 映射为 recommendation.paths。Go 按当前已检查的路径
与完整学校快照计算，不再 fanout 调用 SchoolPicker 模型。
display.sections 保持原板块名与数组顺序，quota 不伪造学校 lookup。
以下完整 A/B/C 例子保留领域含义，按当前 edit 工具外层提交。""", "replace multi-agent fanout; preserve all long examples")
        replace("## 16. 合成 Few-shot\n\n## 16. 合成 Few-shot", "## 16. 合成 Few-shot", "P14: remove duplicated heading only")
        replace("# S3 · 决策树 + 推荐路径配置产出器", "# 决策树、推荐路径与展示", "single Agent domain title")
    elif stem == "school_picker":
        replace("# SCHOOL_PICKER Agent", "# 学校推荐规则与确定性计算", "remove separate model identity")
        replace("- 你只输出 JSON，**不允许任何代码、tool_call、自由文本、自我解释**；", "- 通过 editStrategyDraft.recommendation 编辑路径；Go 计算学校，模型不输出代码或逐分结果；", "replace standalone no-tool protocol")
        replace("- 输入数据不足以产出合法结果时，把异常学校丢到 `stats.invalid_school_names`，并继续输出尽可能多的有效结果。", "- 学校不存在或同名无法唯一解析时报告问题，不能静默丢弃后声称成功。", "P09/P10: unresolved schools fail visibly")
        replace("## 输出契约（强约束 · 只能输出这一个 JSON）", "## 历史计算结果示例（用于理解统计，不由模型提交）", "move old output shape into reference example")
        replace("`match_key=probability` 时按 path_config 的语义处理（如无明确定义，按 score 处理）。", "`match_key=probability` 尚无已验证算法，返回 unsupported_rule，禁止按 score 回退。", "P15: no probability fallback")
        replace("只输出一个 JSON 对象，**外层不要任何 markdown 围栏、不要任何注释**。", "实际执行只编辑路径规则；最终用 __runtime_output 提交本轮结论。", "replace standalone final output")
    return PREAMBLE + text, changes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    source_manifest = json.loads((args.source / "manifest.json").read_text())
    source_hashes = {row["file"]: row["promptSHA256"] for row in source_manifest["prompts"]}
    manifest = {"sourceCommit": source_manifest["commit"], "skills": []}
    for skill, (stem, description) in SOURCES.items():
        raw = (args.source / f"{stem}.md").read_bytes()
        if hashlib.sha256(raw).hexdigest() != source_hashes[f"{stem}.md"]:
            raise ValueError(f"source hash mismatch: {stem}")
        content, changes = migrate(stem, raw.decode())
        header = f"---\ndescription: {description}\nsourceCommit: {source_manifest['commit']}\nsource: {stem}.md\n---\n"
        output = (header + content).encode()
        destination = root / "agents" / "admissions" / "skills" / skill / "SKILL.md"
        if args.check:
            if destination.read_bytes() != output:
                raise ValueError(f"generated Skill changed: {skill}")
        else:
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(output)
        manifest["skills"].append({"name": skill, "source": f"{stem}.md", "sourceSHA256": source_hashes[f"{stem}.md"], "skillSHA256": hashlib.sha256(output).hexdigest(), "bytes": len(output), "changes": changes})
    for skill, source in MANUAL_SKILLS.items():
        destination = root / "agents" / "admissions" / "skills" / skill / "SKILL.md"
        output = destination.read_bytes()
        if not output.strip():
            raise ValueError(f"manual Skill is empty: {skill}")
        manifest["skills"].append({
            "name": skill,
            "source": source,
            "skillSHA256": hashlib.sha256(output).hexdigest(),
            "bytes": len(output),
            "changes": [{
                "kind": "manual",
                "reason": "Track the hand-authored cross-domain review Skill in the same manifest.",
            }],
        })
    dest = root / "agents" / "admissions" / "skill-migration.json"
    output = (json.dumps(manifest, ensure_ascii=False, indent=2) + "\n").encode()
    if args.check:
        if dest.read_bytes() != output:
            raise ValueError("migration manifest mismatch")
    else:
        dest.write_bytes(output)
    print("Verified five source hashes and all six Skills, including the hand-authored review checklist.")


if __name__ == "__main__":
    main()
