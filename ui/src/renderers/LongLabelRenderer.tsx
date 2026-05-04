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

const hasMaxHeight: Tester = (uischema: any) =>
  !!uischema &&
  !!uischema.options &&
  (typeof uischema.options.maxHeight === "number" ||
    typeof uischema.options.maxHeight === "string");

export const longLabelTester = rankWith(
  5,
  and(uiTypeIs("Label"), or(optionIs("collapsible", true), hasMaxHeight))
);

type LongLabelOptions = {
  collapsible?: boolean;
  defaultOpen?: boolean;
  maxHeight?: number | string;
  summary?: string;
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

  const blockStyle: React.CSSProperties = {
    whiteSpace: "pre-wrap",
    lineHeight: 1.6,
    color: "#374151",
    ...(maxHeight !== undefined ? { maxHeight, overflow: "auto" } : {}),
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
        <div style={{ paddingTop: 8, ...blockStyle }}>{text}</div>
      </details>
    );
  }

  return (
    <div className="jsonforms-long-label" style={blockStyle}>
      {text}
    </div>
  );
};

export const LongLabelRenderer = withJsonFormsLayoutProps(Component);
