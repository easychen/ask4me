import React from "react";
import {
  and,
  LabelElement,
  optionIs,
  or,
  rankWith,
  Tester,
  uiTypeIs,
} from "@jsonforms/core";
import { withJsonFormsLayoutProps } from "@jsonforms/react";
import { renderMarkdown } from "./markdown";

const hasMaxHeight: Tester = (uischema: any) =>
  !!uischema &&
  !!uischema.options &&
  (typeof uischema.options.maxHeight === "number" ||
    typeof uischema.options.maxHeight === "string");

export const longLabelTester = rankWith(
  5,
  and(
    uiTypeIs("Label"),
    or(
      optionIs("collapsible", true),
      optionIs("markdown", true),
      hasMaxHeight
    )
  )
);

type LongLabelOptions = {
  collapsible?: boolean;
  defaultOpen?: boolean;
  maxHeight?: number | string;
  summary?: string;
  markdown?: boolean;
};

const Component = ({ uischema, visible }: any) => {
  if (visible === false) return null;

  const label = uischema as LabelElement;
  const opts = (label.options || {}) as LongLabelOptions;
  const text = label.text ?? "";
  const maxHeight =
    typeof opts.maxHeight === "number" || typeof opts.maxHeight === "string"
      ? opts.maxHeight
      : undefined;

  const useMarkdown = opts.markdown === true;
  const blockStyle: React.CSSProperties = {
    ...(useMarkdown ? {} : { whiteSpace: "pre-wrap" }),
    lineHeight: 1.6,
    color: "#374151",
    ...(maxHeight !== undefined ? { maxHeight, overflow: "auto" } : {}),
  };

  const renderBody = (extraStyle?: React.CSSProperties) => {
    const style = { ...blockStyle, ...(extraStyle || {}) };
    if (useMarkdown) {
      return (
        <div
          className="jsonforms-long-label markdown-body"
          style={style}
          dangerouslySetInnerHTML={{ __html: renderMarkdown(text) }}
        />
      );
    }
    return (
      <div className="jsonforms-long-label" style={style}>
        {text}
      </div>
    );
  };

  if (opts.collapsible) {
    const summaryText = opts.summary ?? "详情";
    return (
      <details
        className="jsonforms-collapsible-label"
        open={opts.defaultOpen === true}
        style={{
          border: "1px solid #e5e7eb",
          borderRadius: 6,
          padding: "6px 10px",
          margin: "8px 0",
        }}
      >
        <summary
          style={{
            cursor: "pointer",
            fontWeight: 600,
            padding: "4px 0",
            userSelect: "none",
          }}
        >
          {summaryText}
        </summary>
        {renderBody({ paddingTop: 8 })}
      </details>
    );
  }

  return renderBody();
};

export const LongLabelRenderer = withJsonFormsLayoutProps(Component);
