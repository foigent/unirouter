UniRouter Dashboard v2 Wireframe
Version: 1.0


Status: Ready for Design & Frontend


Target User: Enterprise AI Platform Team


Primary Goal: Understand enterprise AI activity in under 10 seconds.


Secondary Goal: Identify anomalies in under 30 seconds.




Dashboard Philosophy
Dashboard is not a settings homepage.


Dashboard is not a reporting center.


Dashboard is an operational overview of enterprise AI activity.


Every section must answer:


What is happening?


Is it healthy?


What needs attention?




Layout
Screen Width


1440px+



Structure


Header


↓


KPI Row




                                                    1
↓


Activity Trend


↓


Model Activity + Channel Health


↓


Recent Events + Cost Overview


↓


Recent Requests




Section 0
Global Header


Height


56px



Left


Dashboard


Subtitle:


Enterprise AI Activity Overview



Right


Time Range Selector


24h


7d


30d


Custom




                                  2
Refresh Button


Last Updated




Section 1
KPI Row


Height


120px


Grid


4 columns



Card 1


Total Requests


Large Number


2,493,812


Delta


+12.5%


Compared to yesterday



Subtitle


AI Requests



Card 2


Success Rate


99.82%



Color




                        3
Success



Delta


+0.12%



Card 3


Average Latency


127 ms



Color


Warning


if > threshold



Delta


-8%



Card 4


Today's Cost


¥1,248



Delta


+15%



Budget Status


Optional Progress Bar




Section 2
AI Activity Trend




                        4
Height


320px


Full Width



Title


AI Activity Trend



Chart


Area Chart


Use existing:


daily_trend



Metrics


Requests


Primary



Cost


Optional Toggle



Latency


Optional Toggle



Legend


Top Right



Purpose


Answer:


Is AI activity increasing?




                             5
Are there abnormal spikes?




Section 3
Model Activity


Width


70%


Height


280px



Title


Top Models



Visualization


Horizontal Bar Chart


Preferred



Columns


Model


Requests


Cost


Success Rate



Example


GPT-5 1.2M ¥532 99.9%


Claude Sonnet 830K ¥318 99.8%


Gemini 221K ¥91 99.5%




                                6
Purpose


Answer:


Where is AI traffic going?



Future


Can evolve into:


AI Usage Explorer




Section 4
Channel Health


Width


30%


Height


280px



Title


Channel Health



List


OpenAI


Healthy


127ms



Anthropic


Healthy


102ms




                             7
Azure OpenAI


Warning


421ms



OpenRouter


Circuit Open



Display


Status Dot


Provider Name


Latency


Circuit State



Purpose


Answer:


Can requests be routed safely?




Section 5
Recent Events


Width


50%


Height


320px



Title


Recent Events




                                 8
Timeline



Examples


13:41


OpenAI channel recovered



13:38


Low balance threshold reached



13:27


429 spike detected



13:11


User key disabled



Event Types


Info


Warning


Critical



Purpose


Answer:


What changed recently?




Section 6
Cost Overview


Width




                                9
50%


Height


320px



Title


Cost Distribution



Chart


Donut Chart



Group By


Model


Default



Alternative


Provider


Future



Display


GPT-5


52%



Claude


31%



Gemini


11%




                    10
Others


6%



Purpose


Answer:


Where is money being spent?




Section 7
Recent Requests


Height


420px


Full Width



Title


Recent Requests



Data Source


recent_logs



Columns


Request ID


Model


Provider


Status


Latency


Tokens




                              11
Cost


Time



Interactions


Click Row


Open Drawer



Drawer Content


Request Metadata


Cost Breakdown


Headers


Future Trace Link



Purpose


Quick operational investigation




Empty States
No Requests


Show onboarding guidance



No Cost Data


Show billing setup hint



No Channels


Show create channel action




                                  12
Loading States
Skeleton Cards


Skeleton Charts


Skeleton Table



Never show blank screen.




Responsive Rules
1440+


Full Layout



1280


Keep layout


Reduce gutters



1024


Stack


Section 3 and 4


Section 5 and 6



Below 1024


Read-only mode


No operational workflow




Removed From Dashboard
Model Pricing Table




                           13
Move to Billing



Raw Configuration Data


Move to Settings



API Key Counts


Move to Traffic



User Counts


Move to Access



Dashboard must focus on activity.


Not configuration.




Dashboard Success Test
A new operator should answer:


What is the current traffic volume?


Within 5 seconds.



Is the platform healthy?


Within 10 seconds.



Which models dominate usage?


Within 15 seconds.



Which provider is unhealthy?


Within 20 seconds.




                                      14
What happened recently?


Within 30 seconds.



If any answer takes longer than this, the dashboard has failed.




                                                    15
