import { Type } from 'typebox';

const AskQuestionItemParams = Type.Object({
    question: Type.String(),
    options: Type.Array(Type.String(), {
        minItems: 1,
        maxItems: 8,
    }),
    multiSelect: Type.Optional(Type.Boolean()),
    label: Type.Optional(Type.String({ description: '简短导航标签。' })),
});

// 不使用 Type.Union，以兼容 Google 工具 schema。
export const AskQuestionParams = Type.Object({
    question: Type.Optional(Type.String({ description: '单题与 options 搭配；多题用 questions。' })),
    options: Type.Optional(Type.Array(Type.String(), {
        minItems: 1,
        maxItems: 8,
    })),
    multiSelect: Type.Optional(Type.Boolean()),
    questions: Type.Optional(Type.Array(AskQuestionItemParams, {
        minItems: 1,
        maxItems: 8,
    })),
});

