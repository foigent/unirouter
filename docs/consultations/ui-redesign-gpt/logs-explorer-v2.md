UniRouter Logs Explorer v2
Version: 1.0


Status: Production Ready Specification


Audience: Product Design Frontend


Priority: P0


Reason:


Logs Explorer is the future foundation of:


    • Trace
    • X-Insight
    • AI Activity Explorer
    • Cost Analytics
    • Governance Analytics




Product Goal
Logs Explorer answers:


What happened?


Why did it happen?


Who triggered it?


How much did it cost?


Should I take action?




Core Philosophy
Do not design a log table.


Design an investigation workspace.




                                             1
User Scenarios
Scenario 1


Request failed.


Find reason.



Scenario 2


Cost increased.


Find source.



Scenario 3


Latency increased.


Find bottleneck.



Scenario 4


Provider unstable.


Find impact.



Scenario 5


Audit usage.


Find responsible user.




Layout
Three Panel Layout



Filters


|




                         2
Results


|


Details




Width


Left


280px


Center


Flexible


Right


420px




Header
Height


56px



Title


Logs Explorer



Subtitle


Enterprise AI Activity Investigation



Right Actions


Refresh


Export


Saved Views




                                       3
Global Search
Top Search Bar


Full Width



Placeholder


Search Request ID, User, Model, Provider...



Supports


Request ID


Model


Provider


User


Error Code


Future Trace ID



Shortcut


⌘K


or


Ctrl+K




Left Panel
Investigation Filters



Section


Time Range




                                              4
Last 15 min


1 hour


24 hours


7 days


Custom



Section


Model


Multi Select



Section


Provider


Multi Select



Section


Status


Success


Client Error


Provider Error


Timeout


Rate Limit



Section


Latency


0-100ms


100-500ms




                 5
500-1000ms


1000ms+



Section


Cost


Low


Medium


High



Section


Future


User


Organization


Agent


Trace




Center Panel
Results Explorer



Top Toolbar


Result Count


Refresh


Column Settings


Density Switch



Density




                   6
Compact


Comfortable



Default


Compact



Table Columns


Status


Request ID


Model


Provider


Latency


Tokens


Cost


Time



Status


Dot


Green


Yellow


Red



Request ID


Monospace


Copy Button



Model




                7
Tag



Provider


Tag



Latency


Colored


Normal


Warning


Critical



Cost


Currency



Time


Relative


Absolute Toggle




Row Interaction
Single Click


Select Row



Double Click


Open Detail Drawer



Keyboard


Up




                     8
Down


Enter


Esc




Detail Panel
Right Drawer


Width


420px



Header


Request ID


Copy Button


Open Full Page



Section 1


Summary


Model


Provider


Status


Latency


Cost


Tokens


Timestamp



Section 2


Request Metadata




                   9
Request ID


Channel


API Key


Future User


Future Org


Future Agent



Section 3


Token Breakdown


Prompt Tokens


Completion Tokens


Total Tokens



Section 4


Cost Breakdown


Input Cost


Output Cost


Total Cost



Section 5


Response Metadata


Status Code


Retry Count


Provider Response Time



Section 6




                         10
Future Trace


Reserved Area


Trace Graph


Timeline


Dependencies




Empty State
No Results



Message


No activity found



Action


Adjust filters




Saved Views
Future Compatible



Examples


Failed Requests


High Cost Requests


Slow Requests


GPT-5 Activity


Provider Errors



Users can save filter presets.




                                 11
Explorer Modes
Mode 1


Activity


Default



Shows


All Requests



Mode 2


Errors


Only Failed Requests



Mode 3


Cost


Sort by Cost



Mode 4


Latency


Sort by Latency



Mode 5


Future


Trace




Quick Insights Bar
Above Table




                       12
Height


48px



Requests


Success Rate


Average Latency


Average Cost



Updates with filters.



Example


Current Filter:


GPT-5


Last 24h



Display


34,291 Requests


99.2% Success


182ms


¥0.021 Avg Cost




Color Semantics
Success


Green



Warning


Orange




                        13
Failure


Red



Information


Blue



Insight


Purple



Never use color for decoration.




Future Evolution Path
Current


Request Logs


↓


Trace Explorer


↓


AI Activity Explorer


↓


X-Insight


↓


AI Intelligence Console



No redesign required.


Only additional panels.




                                  14
Anti Patterns
Do not add charts everywhere.


Do not create another dashboard.


Do not show configuration data.


Do not overload table columns.


Do not add decorative animations.




Success Metrics
Operator finds failed request


< 30 seconds



Operator finds cost anomaly


< 60 seconds



Operator identifies unhealthy provider


< 60 seconds



Operator understands AI activity trend


< 120 seconds




Final Principle
Logs Explorer is not a log viewer.


Logs Explorer is the operational workspace for understanding enterprise AI activity.




                                                    15
