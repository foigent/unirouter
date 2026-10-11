import { useEffect, useState, type ComponentProps } from 'react';
import { Button, Form, Tooltip } from '@douyinfe/semi-ui';
import { IconEyeClosed, IconEyeOpened } from '@douyinfe/semi-icons';
import { useI18n } from '../../i18n';

type FormInputProps = ComponentProps<typeof Form.Input>;

export type PasswordFieldProps = Omit<FormInputProps, 'mode' | 'suffix' | 'type'> & {
  visibilityResetKey?: unknown;
};

export default function PasswordField({ visibilityResetKey, ...inputProps }: PasswordFieldProps) {
  const { t } = useI18n();
  const [visible, setVisible] = useState(false);
  const toggleLabel = t(visible ? '隐藏密码' : '显示密码');

  useEffect(() => {
    setVisible(false);
  }, [visibilityResetKey]);

  return (
    <Form.Input
      {...inputProps}
      type={visible ? 'text' : 'password'}
      suffix={(
        <Tooltip content={toggleLabel}>
          <Button
            aria-label={toggleLabel}
            aria-pressed={visible}
            htmlType="button"
            icon={visible ? <IconEyeClosed /> : <IconEyeOpened />}
            onClick={() => setVisible((current) => !current)}
            onMouseDown={(event) => event.preventDefault()}
            size="small"
            theme="borderless"
            type="tertiary"
          />
        </Tooltip>
      )}
    />
  );
}
