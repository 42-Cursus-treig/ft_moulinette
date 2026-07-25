#include "ft_list.h"
#include <unistd.h>
#include <stdlib.h>

void	ft_list_merge(t_list **begin_list1, t_list *begin_list2);

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_list_str(t_list *lst)
{
	while (lst)
	{
		put_str((char *)lst->data);
		write(1, "\n", 1);
		lst = lst->next;
	}
}

static t_list	*build(char **items, int n)
{
	t_list	*begin;
	t_list	*elem;
	int		i;

	begin = NULL;
	i = n - 1;
	while (i >= 0)
	{
		elem = ft_create_elem(items[i]);
		elem->next = begin;
		begin = elem;
		i--;
	}
	return (begin);
}

int	main(int argc, char **argv)
{
	t_list	*b1;
	t_list	*b2;
	int		sep;
	int		i;

	sep = -1;
	i = 1;
	while (i < argc)
	{
		if (argv[i][0] == '-' && argv[i][1] == '-' && argv[i][2] == '\0')
		{
			sep = i;
			break ;
		}
		i++;
	}
	if (sep < 0)
		return (0);
	b1 = build(argv + 1, sep - 1);
	b2 = build(argv + sep + 1, argc - sep - 1);
	ft_list_merge(&b1, b2);
	print_list_str(b1);
	return (0);
}
