import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import CompositeRouteRulesEditor from "../CompositeRouteRulesEditor.vue";
import type { CompositeRouteRule } from "@/types";
import { normalizeCompositeRouteRules } from "@/views/admin/groupsCompositeRouteRules";

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

const SelectStub = {
  props: ["id", "modelValue", "options", "disabled"],
  emits: ["update:modelValue"],
  template:
    '<select :id="id" :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
};

const rule = (
  upstream: string,
  conditions: CompositeRouteRule["conditions"] = {},
): CompositeRouteRule => ({
  name: upstream,
  enabled: true,
  target_platform: "openai",
  upstream_model: upstream,
  conditions,
});

function mountEditor(
  rules: CompositeRouteRule[],
  extraProps: Record<string, unknown> = {},
) {
  return mount(CompositeRouteRulesEditor, {
    props: {
      modelValue: rules,
      platformOptions: [
        { value: "openai", label: "OpenAI" },
        { value: "gemini", label: "Gemini" },
      ],
      defaultPlatform: "openai",
      ...extraProps,
    },
    global: {
      stubs: { Icon: true, Toggle: true, Select: SelectStub },
    },
  });
}

const lastEmitted = (wrapper: ReturnType<typeof mountEditor>) =>
  wrapper.emitted("update:modelValue")?.at(-1)?.[0] as
    | CompositeRouteRule[]
    | undefined;

describe("CompositeRouteRulesEditor", () => {
  it("omits tri-state conditions set back to any and keeps explicit yes/no", async () => {
    const wrapper = mountEditor([
      rule("gemini-2.5-pro", { has_images: true, reasoning: false }),
    ]);

    expect(
      (wrapper.get("#composite-rule-0-has_images").element as HTMLSelectElement).value,
    ).toBe("yes");

    await wrapper.get("#composite-rule-0-has_images").setValue("any");
    const cleared = lastEmitted(wrapper)?.[0].conditions;
    expect(cleared).toEqual({ reasoning: false });
    expect(cleared && "has_images" in cleared).toBe(false);

    await wrapper.get("#composite-rule-0-has_tools").setValue("no");
    expect(lastEmitted(wrapper)?.[0].conditions).toEqual({
      has_images: true,
      has_tools: false,
      reasoning: false,
    });
  });

  it("serializes unlimited conditions by omitting their fields", () => {
    const [normalized] = normalizeCompositeRouteRules([
      {
        name: "  ",
        enabled: true,
        target_platform: "gemini",
        upstream_model: " gemini-2.5-pro ",
        conditions: {
          has_images: null,
          has_tools: undefined,
          reasoning: true,
          min_input_tokens: 0,
          max_input_tokens: 0,
          clients: [],
          keywords: [" 翻译 ", "翻译", ""],
        },
      },
    ]);

    expect(normalized).toEqual({
      enabled: true,
      target_platform: "gemini",
      upstream_model: "gemini-2.5-pro",
      conditions: { reasoning: true, keywords: ["翻译"] },
    });
    expect(Object.keys(normalized)).not.toContain("name");
  });

  it("moves rules up and down", async () => {
    const rules = [rule("a"), rule("b"), rule("c")];
    const wrapper = mountEditor(rules);

    expect(
      wrapper.get('[data-testid="composite-rule-move-up-0"]').attributes("disabled"),
    ).toBeDefined();
    expect(
      wrapper.get('[data-testid="composite-rule-move-down-2"]').attributes("disabled"),
    ).toBeDefined();

    await wrapper.get('[data-testid="composite-rule-move-down-0"]').trigger("click");
    expect(lastEmitted(wrapper)?.map((item) => item.upstream_model)).toEqual([
      "b",
      "a",
      "c",
    ]);

    await wrapper.get('[data-testid="composite-rule-move-up-2"]').trigger("click");
    expect(lastEmitted(wrapper)?.map((item) => item.upstream_model)).toEqual([
      "a",
      "c",
      "b",
    ]);
    // 源数组不被就地修改
    expect(rules.map((item) => item.upstream_model)).toEqual(["a", "b", "c"]);
  });

  it("shows an inline error when min input tokens exceed max", async () => {
    const wrapper = mountEditor([
      rule("long-context", { max_input_tokens: 1000 }),
    ]);
    expect(
      wrapper.find('[data-testid="composite-rule-token-error-0"]').exists(),
    ).toBe(false);

    await wrapper
      .get('[data-testid="composite-rule-min-tokens-0"]')
      .setValue("5000");
    const updated = lastEmitted(wrapper);
    expect(updated?.[0].conditions).toEqual({
      min_input_tokens: 5000,
      max_input_tokens: 1000,
    });

    await wrapper.setProps({ modelValue: updated });
    const error = wrapper.get('[data-testid="composite-rule-token-error-0"]');
    expect(error.text()).toBe(
      "admin.groups.compositeRoutes.rules.errors.minGreaterThanMax",
    );
    expect(error.attributes("role")).toBe("alert");
  });

  it("only reports a missing upstream model once validation is requested", async () => {
    const wrapper = mountEditor([rule("")]);
    expect(wrapper.text()).not.toContain(
      "admin.groups.compositeRoutes.rules.errors.upstreamModelRequired",
    );

    await wrapper.setProps({ showValidation: true });
    expect(wrapper.text()).toContain(
      "admin.groups.compositeRoutes.rules.errors.upstreamModelRequired",
    );
  });
});
