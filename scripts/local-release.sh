#!/bin/bash
#
# 本地发布脚本 — 当 CI/CD 流水线出问题时，可在 macOS 本地执行发布
# 用法:
#   ./scripts/local-release.sh          # 自动自增版本号 (patch +1)
#   ./scripts/local-release.sh 1.2.3    # 指定版本号
#

set -euo pipefail

# ============== 颜色 ==============
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }

# ============== 切换到项目根目录 ==============
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"
info "项目根目录: $PROJECT_ROOT"

# ============== 前置检查 ==============
info "正在进行前置检查..."

# 检查 git
command -v git &>/dev/null || error "未找到 git，请先安装 git"

# 检查 node
command -v node &>/dev/null || error "未找到 node，请先安装 Node.js 22+"
NODE_VERSION=$(node -v | sed 's/v//' | cut -d. -f1)
if [[ "$NODE_VERSION" -lt 22 ]]; then
  warn "当前 Node.js 版本 $(node -v)，建议使用 v22+"
fi

# 检查 jq (macOS 可通过 brew install jq 安装)
command -v jq &>/dev/null || error "未找到 jq，请运行: brew install jq"

# 检查 yarn
if ! command -v yarn &>/dev/null; then
  warn "未找到 yarn，正在全局安装..."
  npm install -g yarn
fi

# 检查是否在 git 仓库中
git rev-parse --is-inside-work-tree &>/dev/null || error "当前目录不是 git 仓库"

# 检查当前分支是否为 main
CURRENT_BRANCH=$(git branch --show-current)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
  error "当前分支为 '$CURRENT_BRANCH'，该脚本只能在 main 分支上运行"
fi

# 检查工作区是否干净
if [[ -n "$(git status --porcelain)" ]]; then
  warn "工作区存在未提交的更改:"
  git status --short
  echo ""
  read -p "是否继续？(y/N) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    error "已取消操作"
  fi
fi

ok "前置检查通过"

# ============== 获取完整 tag 历史 ==============
info "拉取远程信息..."
git fetch --tags --force
git fetch origin main

# ============== 确保 main 分支是最新的 ==============
info "拉取 main 分支最新代码..."
git pull origin main --ff-only || error "拉取 main 失败，可能存在冲突，请手动处理"
ok "main 分支已更新到最新"

# ============== 先确保本地 release 分支是最新的 ==============
DEPLOY_BRANCH="release"
info "切换到 $DEPLOY_BRANCH 分支，拉取最新代码..."

git fetch origin "$DEPLOY_BRANCH" 2>/dev/null || warn "远程分支 $DEPLOY_BRANCH 不存在，稍后将创建"

if git show-ref --verify --quiet "refs/remotes/origin/$DEPLOY_BRANCH"; then
  git checkout "$DEPLOY_BRANCH"
  git pull origin "$DEPLOY_BRANCH" --ff-only || error "拉取 $DEPLOY_BRANCH 失败，可能存在冲突，请手动处理"
  ok "$DEPLOY_BRANCH 分支已更新到最新"
  git checkout main
  info "已切回 main 分支，继续打包流程"
else
  info "远程 $DEPLOY_BRANCH 分支不存在，稍后将创建"
fi

# ============== 版本号处理 ==============
INPUT_VERSION="${1:-}"

if [[ -n "$INPUT_VERSION" ]]; then
  # 用户指定版本号，去掉可能带的 v 前缀再加回去
  INPUT_VERSION="${INPUT_VERSION#v}"
  VERSION="v${INPUT_VERSION}"
else
  # 自动从 tag 自增
  LATEST_TAG=$(git tag --sort=-v:refname | grep -E "^v[0-9]+\.[0-9]+\.[0-9]+$" | head -n 1 || true)
  if [[ -z "$LATEST_TAG" ]]; then
    VERSION="v0.0.1"
  else
    VERSION_NUM=${LATEST_TAG#v}
    MAJOR=$(echo "$VERSION_NUM" | cut -d. -f1)
    MINOR=$(echo "$VERSION_NUM" | cut -d. -f2)
    PATCH=$(echo "$VERSION_NUM" | cut -d. -f3)
    PATCH=$((PATCH + 1))
    VERSION="v${MAJOR}.${MINOR}.${PATCH}"
  fi
fi

echo ""
echo -e "============================================"
echo -e "  本次发布版本号: ${GREEN}${VERSION}${NC}"
echo -e "============================================"
echo ""

read -p "确认发布该版本？(y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  error "已取消发布"
fi

# ============== 安装依赖 ==============
info "安装依赖..."
yarn install --ignore-engines

# ============== 构建 ==============
info "开始构建..."
yarn build

# ============== 打包产物 ==============
info "打包发布产物..."
rm -rf dist-release
mkdir -p dist-release

cp -rf ./lib dist-release/lib
cp -rf ./package.json dist-release/package.json
cp -rf ./README.md dist-release/README.md

# .puppeteerrc.cjs 可能不存在，按需复制
if [[ -f ./.puppeteerrc.cjs ]]; then
  cp -rf ./.puppeteerrc.cjs dist-release/.puppeteerrc.cjs
fi

# 检查产物
if [[ ! -d dist-release || -z $(ls -A dist-release) ]]; then
  error "'dist-release' 目录不存在或为空，构建步骤可能失败"
fi

# 移到上级目录避免切换分支时被清理
mv dist-release ../dist-release
ok "构建产物准备完成"

# ============== 清理函数 (异常时恢复) ==============
ORIGINAL_BRANCH="$CURRENT_BRANCH"
cleanup() {
  echo ""
  warn "执行清理..."
  # 尝试切回原分支
  git checkout "$ORIGINAL_BRANCH" 2>/dev/null || true
  # 清理可能残留的 dist-release
  rm -rf ../dist-release 2>/dev/null || true
  warn "已切回 $ORIGINAL_BRANCH 分支"
}
trap cleanup ERR

# ============== 切换到 release 分支 ==============
info "准备推送到 $DEPLOY_BRANCH 分支..."

if ! git show-ref --verify --quiet "refs/heads/$DEPLOY_BRANCH"; then
  # 本地不存在 release 分支，创建
  git checkout -b "$DEPLOY_BRANCH"
  git push origin "$DEPLOY_BRANCH"
else
  # 本地已存在且前面已 pull 过最新，直接切换
  git checkout "$DEPLOY_BRANCH"
fi

# ============== 清理工作区 ==============
info "清理工作区..."
rm -rf node_modules
rm -rf .gitignore

# 清理非 git 文件夹的其他内容（保护 .git 目录）
find . -mindepth 1 ! -path './.git*' -exec rm -rf {} + 2>/dev/null || true

# 移动构建产物到根目录
mv ../dist-release/* ./ || error "无法将 'dist-release' 目录中的文件移动到根目录，目录可能为空"
rm -rf ../dist-release 2>/dev/null || true

# 检查 .git
if [[ ! -d .git ]]; then
  error ".git 目录已丢失，无法进行后续 git 操作"
fi

# ============== 清理 package.json ==============
info "清理 package.json..."
jq 'del(.devDependencies)' package.json > package.json.tmp && mv package.json.tmp package.json
jq 'del(.workspaces)' package.json > package.json.tmp && mv package.json.tmp package.json
jq 'del(.private)' package.json > package.json.tmp && mv package.json.tmp package.json
jq 'del(.scripts)' package.json > package.json.tmp && mv package.json.tmp package.json
jq --arg version "$VERSION" '.version = ($version | sub("^v";""))' package.json > package.json.tmp && mv package.json.tmp package.json

ok "package.json 已清理"

# ============== 提交并推送 ==============
info "提交并推送到 $DEPLOY_BRANCH..."
git config --local user.email "ningmengchongshui@gmail.com"
git config --local user.name "ningmengchongshui"
git config --local push.autoSetupRemote true
git add -A
git commit -m "$VERSION"
git push origin "$DEPLOY_BRANCH" || error "推送失败，请检查网络或权限"

ok "已推送到 $DEPLOY_BRANCH 分支"

# ============== 打 tag ==============
info "创建并推送 tag: $VERSION"
git tag -f "$VERSION"
git push origin "$VERSION" --force

ok "tag $VERSION 已推送"

# ============== 切回原分支 ==============
info "切回 $ORIGINAL_BRANCH 分支..."
git checkout "$ORIGINAL_BRANCH"

# 恢复 node_modules
info "重新安装依赖..."
yarn install --ignore-engines

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}  发布完成！版本: ${VERSION}${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo "  - 分支: $DEPLOY_BRANCH"
echo "  - 标签: $VERSION"
echo ""
