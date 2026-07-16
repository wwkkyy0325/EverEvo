package catalog

// TaskZH 标准 task 标签的中文映射表。
// 覆盖 HuggingFace pipeline_tag 和 ModelScope task 的常见值。
var TaskZH = map[string]string{
	// 图像
	"image-classification":           "图像分类",
	"image-segmentation":             "图像分割",
	"object-detection":               "目标检测",
	"image-to-text":                  "图像描述",
	"text-to-image":                  "文生图",
	"image-to-image":                 "图生图",
	"unconditional-image-generation": "图像生成",
	"depth-estimation":               "深度估计",
	"panoptic-segmentation":          "全景分割",
	"semantic-segmentation":          "语义分割",
	"instance-segmentation":          "实例分割",
	"zero-shot-image-classification": "零样本图像分类",
	"mask-generation":                "遮罩生成",
	"video-classification":           "视频分类",
	"image-feature-extraction":       "图像特征提取",

	// 文本/NLP
	"text-generation":               "文本生成",
	"text2text-generation":          "文本转换",
	"text-classification":           "文本分类",
	"token-classification":          "命名实体识别",
	"question-answering":            "问答系统",
	"translation":                   "翻译",
	"summarization":                 "摘要生成",
	"fill-mask":                     "填空预测",
	"feature-extraction":            "特征提取",
	"sentence-similarity":           "句子相似度",
	"zero-shot-classification":      "零样本分类",
	"conversational":                "对话",
	"table-question-answering":      "表格问答",
	"multiple-choice":               "多选题",
	"text-ranking":                  "文本排序",
	"other":                         "其他",

	// 语音
	"automatic-speech-recognition":  "语音识别",
	"text-to-speech":                "语音合成",
	"audio-classification":          "音频分类",
	"audio-to-audio":                "音频转换",
	"voice-activity-detection":      "语音活动检测",
	"speech-to-text":                "语音转文字",

	// 多模态
	"image-text-to-text":            "图文理解",
	"visual-question-answering":     "视觉问答",
	"document-question-answering":   "文档问答",
	"multimodal":                    "多模态",

	// LLM 相关
	"chat":                          "对话",
	"instruction-tuned":             "指令微调",
	"rlhf":                          "人类反馈强化学习",

	// 其他
	"reinforcement-learning":        "强化学习",
	"robotics":                      "机器人",
	"time-series-forecasting":       "时间序列预测",
	"tabular":                       "表格数据",
}

// TranslateTask 返回 task 的中文名，未映射则返回原文。
func TranslateTask(task string) string {
	if zh, ok := TaskZH[task]; ok {
		return zh
	}
	return task
}
