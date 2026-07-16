# Vue 3 Options API → `<script setup>` 迁移大修

> 全量迁移 30 个组件（2026-07-04），记录遇到的坑和模式。

## 1. `$emit` 在 `<script setup>` 模板中不总是可用

### 问题
Vue 3.3+ 文档声明 `$emit` 在 `<script setup>` 模板中可用，但实际在递归组件和箭头函数闭包中不生效。

```html
<!-- ❌ 不生效 -->
<button @click="$emit('download', node.path)">下载</button>
<Child @toggle="p => $emit('toggle', p)" />

<!-- ✅ 必须用 defineEmits 返回的函数 -->
<button @click="emit('download', node.path)">下载</button>
<Child @toggle="p => emit('toggle', p)" />
```

### 影响范围
FileTreeNode, PackageTreeNode, ModelCard, ModelList, SimilarityTool, ImageClassifierTool, PluginUse, ToastContainer — 8 个文件，所有递归事件转发和直接 emit 调用。

### 正确模式
```ts
const emit = defineEmits<{ 'toggle-dir': [path: string]; download: [path: string] }>()
// 模板中直接用 emit(...)，不要用 $emit
```

---

## 2. Props 解构成 `const` 丢响应式 ⚡ 高频

### 问题
Props 是响应式的，但解构成局部 `const` 后只捕获初始值，永远不会再更新。

```ts
// ❌ 死代码 — 只取了第一次 props 的值
const expandedDirs = props.expandedDirs ?? {}
const checkedFiles = props.checkedFiles ?? {}
const dlState = props.dlState ?? {}

// 父组件改了 props.expandedDirs，但这里读的还是初始空对象
const expanded = computed(() => expandedDirs[path]) // 永远读到 undefined
```

### 正确模式
```ts
// ✅ 在 computed 内部直接读 props.xxx
const expanded = computed(() => (props.expandedDirs ?? {})[path])

// ✅ 或如果需要多个地方用到，用 computed 包装
const expandedDirs = computed(() => props.expandedDirs ?? {})
const expanded = computed(() => expandedDirs.value[path])
```

### 检查方法
```bash
grep -n "const.*= props\." **/*.vue | grep -v "computed\|toRef\|ref("
```
任何匹配行如果不在 `computed(() => ...)` 或 `toRef(props, ...)` 里，就是 bug。

---

## 3. `data()` 对象 → `ref()` vs `reactive()`

### 选择规则
| 原始类型、数组 | `ref()` |
|---------------|---------|
| 表单对象（多个字段一起修改） | `reactive()` |
| 键值字典（`Record<string, boolean>`） | `ref({})` 然后用 `.value = {...}` 整体替换 |
| 纯 `Map`（非响应式缓存） | 原生 `new Map()` |

### 反模式
```ts
// ❌ reactive({}) 直接新增 key 有时不触发更新
const expanded = reactive<Record<string, boolean>>({})
expanded[key] = true  // Vue 3 Proxy 理论上支持，但递归组件中不稳定

// ✅ ref() + 整体替换，强制触发
const expanded = ref<Record<string, boolean>>({})
expanded.value = { ...expanded.value, [key]: true }
```

---

## 4. `this.$root.showToast` → `getCurrentInstance()`

### 问题
迁移后 `this.$root` 在 `<script setup>` 中不可用，必须通过 `getCurrentInstance()`。

```ts
// ✅ 推荐：抽成工具函数
function getRoot() {
  return (getCurrentInstance()?.proxy as any)?.$root
}
function t(type: string, title: string, desc = '') {
  try { getRoot()?.showToast?.(type, title, desc) } catch (_) {}
}
```

**未来改进**：替换为 Pinia store 或 provide/inject，消除对 `$root` 的依赖。

---

## 5. KeepAlive 组件名匹配

`<script setup>` 组件没有显式 `name`，Vue 从文件名推断。KeepAlive 的 `include` 数组需要匹配推断出的名字：

```html
<!-- Vue 推断 FileTreeNode.vue → name: "FileTreeNode" -->
<KeepAlive :include="['ModelCatalog', 'MyModels']">
  <RouterView v-slot="{ Component }">
    <component :is="Component" />
  </RouterView>
</KeepAlive>
```

如果需要精确控制 name，用 `defineOptions`：
```ts
defineOptions({ name: 'FileTreeNode' })
```

---

## 6. Router 迁移导致 props 丢失

旧版 `v-show` 模式中 App.vue 直接向子组件传递 props：
```html
<MyModels :models="models" @load="onLoadModel" @unload="onUnloadModel" />
```

RouterView 模式下不再传递 props。**每个页面组件必须自给自足**：
- 自己调用 API 获取数据
- 直接调用 backend 方法代替 emit 给父组件

---

## 验证清单

迁移后每个组件必须通过：
- [ ] 所有 `@click` 事件有响应
- [ ] 展开/收起状态能切换
- [ ] 表单输入能正常更新
- [ ] 列表数据能正常加载和刷新
- [ ] 弹窗能正常打开和关闭
- [ ] `npm run build` 零错误
